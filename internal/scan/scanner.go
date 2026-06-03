package scan

import (
	"fmt"
	"io"
	"log"
	nethttp "net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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
	collected       []proute.ScanResult

	checkpoint          *Checkpoint
	checkpointPath      string
	checkpointInterval  int
	checkpointCounter   int64
	checkpointWordlists []string
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
		ScopeCheck:          config.ScopeCheck,
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

// SetCheckpoint configures checkpoint persistence for this scanner. Call before Run.
// path is the checkpoint file location; interval is how many completed requests to
// process between periodic saves (0 or negative uses the default of 500).
// wordlists records which wordlist files were used (informational, stored in the file).
func (s *Scanner) SetCheckpoint(path string, interval int, wordlists []string) {
	s.checkpointPath = path
	if interval <= 0 {
		interval = 500
	}
	s.checkpointInterval = interval
	s.checkpointWordlists = wordlists
}

// Run executes the full scan: preflight → submit jobs → drain and filter results.
// It returns after all results are consumed or a SIGINT/SIGTERM is received.
func (s *Scanner) Run(targets []proute.ScanTarget, routes []proute.Route) error {
	if len(targets) == 0 || len(routes) == 0 {
		return nil
	}

	// Initialise checkpoint if configured; intercept signals ourselves so we can
	// save state before the pool shuts down.
	if s.checkpointPath != "" {
		if err := s.initCheckpoint(targets); err != nil {
			return err
		}
		s.watchSignalsWithCheckpoint()
	} else {
		s.pool.WatchSignals()
	}

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
		scanRoutes := routes
		if s.checkpoint != nil {
			scanRoutes = s.checkpoint.RemainingRoutes(routes, target)
		}
		for _, route := range scanRoutes {
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

	// Write final checkpoint capturing all completed routes and results.
	if s.checkpoint != nil {
		s.syncQuarantineToCheckpoint()
		if err := s.checkpoint.Save(s.checkpointPath); err != nil {
			log.Printf("[WARN] checkpoint final save: %v", err)
		}
	}

	return nil
}

// initCheckpoint loads an existing checkpoint file (resume) or creates a new one.
func (s *Scanner) initCheckpoint(targets []proute.ScanTarget) error {
	cp := &Checkpoint{}
	if _, err := os.Stat(s.checkpointPath); err == nil {
		// File exists — resume.
		if err := cp.Load(s.checkpointPath); err != nil {
			return fmt.Errorf("load checkpoint %s: %w", s.checkpointPath, err)
		}
		log.Printf("[INFO] Resuming scan %s (started %s) — %d completed, continuing...",
			cp.ScanID, cp.StartedAt.Format(time.RFC3339), len(cp.CompletedKeys))
		// Restore quarantine list from previous run.
		for _, host := range cp.Quarantined {
			s.quarantine.Add(host, "restored from checkpoint")
		}
	} else {
		// New scan.
		cp = NewCheckpoint(s.config, targets, s.checkpointWordlists)
		log.Printf("[INFO] Checkpoint enabled — scan ID %s, writing to %s every %d requests",
			cp.ScanID, s.checkpointPath, s.checkpointInterval)
	}
	s.checkpoint = cp
	return nil
}

// watchSignalsWithCheckpoint registers a SIGINT/SIGTERM handler that saves the
// checkpoint before delegating shutdown to the pool.
func (s *Scanner) watchSignalsWithCheckpoint() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer signal.Stop(ch)
		select {
		case sig := <-ch:
			log.Printf("[INFO] received %s — saving checkpoint before exit", sig)
			s.syncQuarantineToCheckpoint()
			if err := s.checkpoint.Save(s.checkpointPath); err != nil {
				log.Printf("[WARN] checkpoint save on signal: %v", err)
			}
			s.pool.Shutdown()
		case <-s.pool.Done():
		}
	}()
}

// syncQuarantineToCheckpoint copies the current quarantine list into the checkpoint.
func (s *Scanner) syncQuarantineToCheckpoint() {
	if s.checkpoint == nil {
		return
	}
	hosts := s.quarantine.List()
	s.checkpoint.mu.Lock()
	s.checkpoint.Quarantined = hosts
	s.checkpoint.mu.Unlock()
}

// ResultCount returns the number of results that passed all filters and were emitted.
func (s *Scanner) ResultCount() int64 {
	return atomic.LoadInt64(&s.resultCount)
}

// Quarantine exposes the scanner's quarantine registry for inspection.
func (s *Scanner) Quarantine() *Quarantine {
	return s.quarantine
}

// Results returns a snapshot of all collected scan results.
func (s *Scanner) Results() []proute.ScanResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]proute.ScanResult, len(s.collected))
	copy(out, s.collected)
	return out
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

	// Record the route as completed and trigger a periodic checkpoint save.
	if s.checkpoint != nil {
		s.checkpoint.MarkComplete(result.Req.Route.Method, host, result.Req.Route.Path)
		n := atomic.AddInt64(&s.checkpointCounter, 1)
		if int(n)%s.checkpointInterval == 0 {
			if err := s.checkpoint.Save(s.checkpointPath); err != nil {
				log.Printf("[WARN] periodic checkpoint save: %v", err)
			}
		}
	}

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

	s.mu.Lock()
	s.collected = append(s.collected, sr)
	s.mu.Unlock()

	if s.checkpoint != nil {
		s.checkpoint.AddResult(sr)
	}
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
