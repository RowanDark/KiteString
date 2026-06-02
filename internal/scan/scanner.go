package scan

import (
	"io"
	"log"
	nethttp "net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	kshttp "github.com/RowanDark/kitestring/internal/http"
	"github.com/RowanDark/kitestring/internal/output"
	"github.com/RowanDark/kitestring/pkg/proute"
)

// Scanner wires together the HTTP pool, preflight checks, wordlist routes,
// and output layer into a working end-to-end scan.
type Scanner struct {
	config          proute.ScanConfig
	pool            *kshttp.Pool
	preflightClient *nethttp.Client
	quarantine      *Quarantine
	out             *output.Writer
	baselines       map[string]map[string]*Baseline // host → prefix → baseline
	mu              sync.RWMutex
	resultCount     int64
}

// New initialises all scanner components from config and returns a ready Scanner.
func New(config proute.ScanConfig) (*Scanner, error) {
	if config.MaxConnPerHost <= 0 {
		config.MaxConnPerHost = 5
	}
	if config.MaxParallelHosts <= 0 {
		config.MaxParallelHosts = 10
	}
	if config.Timeout <= 0 {
		config.Timeout = 10 * time.Second
	}

	extraHeaders := parseExtraHeaders(config.Headers)

	ksClient := kshttp.NewClient(kshttp.ClientConfig{
		Timeout:             config.Timeout,
		MaxIdleConnsPerHost: config.MaxConnPerHost,
		UserAgent:           config.UserAgent,
		MaxRedirects:        config.MaxRedirects,
		ExtraHeaders:        extraHeaders,
		BlacklistDomains:    config.BlacklistDomains,
	})

	limiter := kshttp.NewRateLimiter(kshttp.RateLimiterConfig{
		Delay:                config.Delay,
		MaxRetries:           config.MaxRetries,
		BaseBackoff:          config.BackoffBase,
		MaxBackoff:           config.BackoffMax,
		UnreachableThreshold: config.UnreachableThreshold,
	})

	pool := kshttp.NewPool(kshttp.PoolConfig{
		MaxConnPerHost:   config.MaxConnPerHost,
		MaxParallelHosts: config.MaxParallelHosts,
		Client:           ksClient,
		Limiter:          limiter,
	})

	return &Scanner{
		config:          config,
		pool:            pool,
		preflightClient: &nethttp.Client{Timeout: config.Timeout},
		quarantine:      NewQuarantine(config.QuarantineThresh),
		out:             output.New(config.OutputFormat, nil),
		baselines:       make(map[string]map[string]*Baseline),
	}, nil
}

// Run executes the full scan: preflight → submit jobs → drain and filter results.
// It returns after all results are consumed or a SIGINT/SIGTERM is received.
func (s *Scanner) Run(targets []proute.ScanTarget, routes []proute.Route) error {
	if len(targets) == 0 || len(routes) == 0 {
		return nil
	}

	s.pool.WatchSignals()

	// Build wildcard baselines for each reachable target before scanning.
	for _, target := range targets {
		if s.quarantine.Check(target.Host) {
			continue
		}
		_, baselines, err := Preflight(target, routes, s.config.PreflightDepth,
			s.config.DisablePreflight, s.preflightClient)
		if err != nil {
			log.Printf("[WARN] preflight %s: %v", target.Host, err)
			continue
		}
		if baselines != nil {
			s.mu.Lock()
			s.baselines[target.Host] = baselines
			s.mu.Unlock()
		}
	}

	// wg tracks submitted jobs: one Add per Submit, one Done per result received.
	// This lets us call pool.Shutdown only after every job has produced a result,
	// avoiding the race where Shutdown cancels in-flight requests.
	var wg sync.WaitGroup

	// Consume results concurrently while jobs are being submitted below.
	go func() {
		for result := range s.pool.Results() {
			wg.Done()
			s.handleResult(result)
		}
	}()

	// Submit jobs synchronously so that all goroutines started by pool.Submit
	// can enqueue into the jobs channel before pool.Shutdown cancels the context.
	for _, target := range targets {
		for _, route := range routes {
			if s.quarantine.Check(target.Host) {
				break // skip remaining routes for a quarantined host
			}
			if s.config.FilterAPIKSUID != "" && route.KSUID != s.config.FilterAPIKSUID {
				continue
			}
			if s.config.ForceMethod != "" {
				route.Method = s.config.ForceMethod
			}
			req, err := kshttp.Build(route, target)
			if err != nil {
				continue
			}
			wg.Add(1)
			s.pool.Submit(req)
		}
	}

	// Wait for all submitted jobs to produce a result, then drain the pool.
	wg.Wait()
	s.pool.Shutdown()

	return nil
}

// ResultCount returns the number of results that passed all filters and were emitted.
func (s *Scanner) ResultCount() int64 {
	return atomic.LoadInt64(&s.resultCount)
}

// Quarantine exposes the scanner's quarantine registry for inspection.
func (s *Scanner) Quarantine() *Quarantine {
	return s.quarantine
}

// SetOutput redirects scan result output to w (useful for testing).
func (s *Scanner) SetOutput(w io.Writer) {
	s.out.SetWriter(w)
}

func (s *Scanner) handleResult(result *kshttp.Result) {
	if result.Err != nil {
		return
	}

	host := result.Req.Target.Host
	if s.quarantine.Check(host) {
		return
	}

	// Wildcard detection: compare result against its path-prefix baseline.
	var baselineBodies []string
	if !s.config.DisablePreflight {
		prefix := prefixAtDepth(result.Req.Route.Path, s.config.PreflightDepth)
		s.mu.RLock()
		hostBaselines := s.baselines[host]
		s.mu.RUnlock()
		if hostBaselines != nil {
			if baseline, ok := hostBaselines[prefix]; ok {
				if s.config.WildcardDetection && isWildcardNormalized(result.Resp, baseline) {
					if s.quarantine.RecordWildcard(host) {
						log.Printf("[WARN] host %s quarantined after consecutive wildcard responses", host)
					}
					return
				}
				if baseline.BodyText != "" {
					baselineBodies = []string{baseline.BodyText}
				}
			}
		}
	}

	fr := Filter(result, s.config, baselineBodies)
	if !fr.Passed {
		if s.config.Verbose == "debug" || s.config.Verbose == "trace" {
			log.Printf("[DEBUG] filtered %s: %s", result.Req.FullURL, fr.Reason)
		}
		return
	}

	sr := proute.ScanResult{
		Target:        result.Req.Target,
		Route:         result.Req.Route,
		StatusCode:    result.Resp.StatusCode,
		ContentLength: int(result.Resp.ContentLength),
		ResponseTime:  result.Resp.Duration,
		Timestamp:     time.Now(),
		URL:           result.Resp.URL,
		KSUID:         result.Req.Route.KSUID,
	}

	s.out.Write(sr)
	atomic.AddInt64(&s.resultCount, 1)
}

func parseExtraHeaders(headers []string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	m := make(map[string]string, len(headers))
	for _, h := range headers {
		idx := strings.IndexByte(h, ':')
		if idx <= 0 {
			continue
		}
		k := strings.TrimSpace(h[:idx])
		v := strings.TrimSpace(h[idx+1:])
		if k != "" {
			m[k] = v
		}
	}
	return m
}
