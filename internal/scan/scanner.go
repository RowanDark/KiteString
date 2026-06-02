package scan

import (
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

const defaultCheckpointInterval = 500

// Scanner wires together the HTTP pool, preflight checks, wordlist routes,
// and output layer into a working end-to-end scan.
type Scanner struct {
	config             proute.ScanConfig
	pool               *kshttp.Pool
	preflightClient    *nethttp.Client
	quarantine         *Quarantine
	out                output.Writer
	baselines          map[string]map[string]*Baseline // host → prefix → baseline
	mu                 sync.RWMutex
	resultCount        int64
	checkpoint         *Checkpoint
	checkpointPath     string
	checkpointInterval int
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
		Scope:               config.Scope,
		Verbose:             config.Verbose,
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
		config:             config,
		pool:               pool,
		preflightClient:    &nethttp.Client{Timeout: config.Timeout},
		quarantine:         NewQuarantine(config.QuarantineThresh),
		out:                mustWriter(config.OutputFormat, nil),
		baselines:          make(map[string]map[string]*Baseline),
		checkpointInterval: defaultCheckpointInterval,
	}, nil
}

// SetCheckpoint wires a checkpoint into the scanner. On run it will skip
// already-completed routes, save periodically every interval requests
// (default 500), and save on SIGINT and on clean completion.
func (s *Scanner) SetCheckpoint(cp *Checkpoint, path string, interval int) {
	s.checkpoint = cp
	s.checkpointPath = path
	if interval > 0 {
		s.checkpointInterval = interval
	}
}

// ReplayResults re-emits all results stored in the checkpoint to the output
// writer. Call this after SetCheckpoint and before Run when resuming so that
// the output contains both previous and new results.
func (s *Scanner) ReplayResults() {
	if s.checkpoint == nil {
		return
	}
	for _, result := range s.checkpoint.Results {
		_ = s.out.WriteResult(result)
	}
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
	var completedSinceLastSave int64

	// doneCh is closed when the scan completes normally, signalling the SIGINT
	// handler goroutine below that checkpoint saving is no longer needed there.
	doneCh := make(chan struct{})

	// On SIGINT/SIGTERM, persist the checkpoint before the pool shuts down.
	if s.checkpoint != nil {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		go func() {
			defer signal.Stop(sigCh)
			select {
			case <-sigCh:
				s.saveCheckpoint()
			case <-doneCh:
			}
		}()
	}

	// Consume results concurrently while jobs are being submitted below.
	go func() {
		for result := range s.pool.Results() {
			wg.Done()
			// Mark complete for every result (error or not) so that resume
			// skips routes that were already attempted.
			if s.checkpoint != nil && result.Req != nil {
				s.checkpoint.MarkComplete(
					result.Req.Route.Method,
					result.Req.Target.Host,
					result.Req.Route.Path,
				)
			}
			s.handleResult(result)
			// Periodic checkpoint save every checkpointInterval completions.
			if s.checkpoint != nil {
				n := atomic.AddInt64(&completedSinceLastSave, 1)
				if int(n) >= s.checkpointInterval {
					atomic.StoreInt64(&completedSinceLastSave, 0)
					s.saveCheckpoint()
				}
			}
		}
	}()

	// Submit jobs synchronously so that all goroutines started by pool.Submit
	// can enqueue into the jobs channel before pool.Shutdown cancels the context.
	for _, target := range targets {
		// When resuming, skip routes that were already completed for this target.
		effectiveRoutes := routes
		if s.checkpoint != nil {
			effectiveRoutes = s.checkpoint.RemainingRoutes(routes, target)
		}
		for _, route := range effectiveRoutes {
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
	close(doneCh)

	// Final checkpoint save on clean completion.
	if s.checkpoint != nil {
		s.saveCheckpoint()
	}

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
	s.out = mustWriter(s.config.OutputFormat, w)
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

	_ = s.out.WriteResult(sr)
	atomic.AddInt64(&s.resultCount, 1)

	if s.checkpoint != nil {
		s.checkpoint.AddResult(sr)
	}
}

// saveCheckpoint updates the quarantine list and writes the checkpoint to disk.
func (s *Scanner) saveCheckpoint() {
	if s.checkpoint == nil {
		return
	}
	s.checkpoint.SetQuarantined(s.quarantine.QuarantinedHosts())
	if err := s.checkpoint.Save(s.checkpointPath); err != nil {
		log.Printf("[WARN] checkpoint save: %v", err)
	} else {
		log.Printf("[INFO] checkpoint saved: %s", s.checkpointPath)
	}
}

func mustWriter(format string, w io.Writer) output.Writer {
	ow, _ := output.NewWriter(format, w)
	return ow
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
