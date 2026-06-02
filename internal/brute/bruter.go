package brute

import (
	"crypto/sha256"
	"io"
	"log"
	nethttp "net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	kshttp "github.com/RowanDark/kitestring/internal/http"
	"github.com/RowanDark/kitestring/internal/output"
	"github.com/RowanDark/kitestring/internal/scan"
	"github.com/RowanDark/kitestring/pkg/proute"
)

// Bruter executes traditional flat path-fuzzing against one or more targets.
type Bruter struct {
	config          proute.ScanConfig
	pool            *kshttp.Pool
	preflightClient *nethttp.Client
	quarantine      *scan.Quarantine
	out             output.Writer
	baselines       map[string]map[string]*scan.Baseline
	mu              sync.RWMutex
	resultCount     int64
}

// New initialises all Bruter components from config and returns a ready Bruter.
func New(config proute.ScanConfig) (*Bruter, error) {
	if config.MaxConnPerHost <= 0 {
		config.MaxConnPerHost = 10
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

	return &Bruter{
		config:          config,
		pool:            pool,
		preflightClient: &nethttp.Client{Timeout: config.Timeout},
		quarantine:      scan.NewQuarantine(config.QuarantineThresh),
		out:             mustWriter(config.OutputFormat, nil),
		baselines:       make(map[string]map[string]*scan.Baseline),
	}, nil
}

// Run executes the brute-force scan: preflight → submit GET requests → drain and filter results.
// Paths are always issued as flat GET requests; method/param context is intentionally ignored.
func (b *Bruter) Run(targets []proute.ScanTarget, paths []string) error {
	if len(targets) == 0 || len(paths) == 0 {
		return nil
	}

	method := nethttp.MethodGet
	if b.config.ForceMethod != "" {
		method = strings.ToUpper(b.config.ForceMethod)
	}

	routes := make([]proute.Route, len(paths))
	for i, p := range paths {
		routes[i] = proute.Route{
			Method: method,
			Path:   p,
		}
	}

	b.pool.WatchSignals()

	for _, target := range targets {
		if b.quarantine.Check(target.Host) {
			continue
		}
		_, baselines, err := scan.Preflight(target, routes, b.config.PreflightDepth,
			b.config.DisablePreflight, b.preflightClient)
		if err != nil {
			log.Printf("[WARN] preflight %s: %v", target.Host, err)
			continue
		}
		if baselines != nil {
			b.mu.Lock()
			b.baselines[target.Host] = baselines
			b.mu.Unlock()
		}
	}

	var wg sync.WaitGroup

	go func() {
		for result := range b.pool.Results() {
			wg.Done()
			b.handleResult(result)
		}
	}()

	for _, target := range targets {
		for _, route := range routes {
			if b.quarantine.Check(target.Host) {
				break
			}
			req, err := kshttp.Build(route, target)
			if err != nil {
				continue
			}
			wg.Add(1)
			b.pool.Submit(req)
		}
	}

	wg.Wait()
	b.pool.Shutdown()

	return nil
}

// ResultCount returns the number of results that passed all filters and were emitted.
func (b *Bruter) ResultCount() int64 {
	return atomic.LoadInt64(&b.resultCount)
}

// SetOutput redirects result output to w (useful for testing).
func (b *Bruter) SetOutput(w io.Writer) {
	b.out = mustWriter(b.config.OutputFormat, w)
}

func (b *Bruter) handleResult(result *kshttp.Result) {
	if result.Err != nil {
		return
	}

	host := result.Req.Target.Host
	if b.quarantine.Check(host) {
		return
	}

	var baselineBodies []string
	if !b.config.DisablePreflight {
		prefix := prefixAtDepth(result.Req.Route.Path, b.config.PreflightDepth)
		b.mu.RLock()
		hostBaselines := b.baselines[host]
		b.mu.RUnlock()
		if hostBaselines != nil {
			if baseline, ok := hostBaselines[prefix]; ok {
				if b.config.WildcardDetection && isWildcard(result.Resp, baseline) {
					if b.quarantine.RecordWildcard(host) {
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

	fr := scan.Filter(result, b.config, baselineBodies)
	if !fr.Passed {
		if b.config.Verbose == "debug" || b.config.Verbose == "trace" {
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

	_ = b.out.WriteResult(sr)
	atomic.AddInt64(&b.resultCount, 1)
}

// isWildcard compares a normalized response against a preflight baseline.
func isWildcard(resp *kshttp.Response, baseline *scan.Baseline) bool {
	if resp.StatusCode != baseline.StatusCode {
		return false
	}
	if resp.ContentLength != baseline.ContentLength {
		return false
	}
	if mimeType(resp.Headers.Get("Content-Type")) != mimeType(baseline.ContentType) {
		return false
	}
	return sha256.Sum256(resp.Body) == baseline.BodyHash
}

// prefixAtDepth returns the first depth path segments as a prefix string.
func prefixAtDepth(path string, depth int) string {
	if depth == 0 {
		return "/"
	}
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return "/"
	}
	parts := strings.SplitN(path, "/", depth+1)
	if len(parts) <= depth {
		return "/" + strings.Join(parts, "/")
	}
	return "/" + strings.Join(parts[:depth], "/")
}

func mustWriter(format string, w io.Writer) output.Writer {
	ow, _ := output.NewWriter(format, w)
	return ow
}

func mimeType(ct string) string {
	if idx := strings.IndexByte(ct, ';'); idx >= 0 {
		return strings.TrimSpace(ct[:idx])
	}
	return strings.TrimSpace(ct)
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
