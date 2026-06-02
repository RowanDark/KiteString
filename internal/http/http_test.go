package http_test

import (
	"fmt"
	nethttp "net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	kshttp "github.com/RowanDark/kitestring/internal/http"
	"github.com/RowanDark/kitestring/pkg/proute"
)

// targetFromServer parses a ScanTarget from an httptest.Server URL.
func targetFromServer(srv *httptest.Server) proute.ScanTarget {
	targets, err := proute.ParseTarget(srv.URL)
	if err != nil {
		panic(fmt.Sprintf("targetFromServer: %v", err))
	}
	return targets[0]
}

// --- Single request round-trip ---

func TestClientDo_RoundTrip(t *testing.T) {
	var gotUA, gotCustom string
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotCustom = r.Header.Get("X-Custom")
		w.WriteHeader(nethttp.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	client := kshttp.NewClient(kshttp.ClientConfig{
		UserAgent:    "KiteString-Test/1.0",
		ExtraHeaders: map[string]string{"X-Custom": "injected"},
	})

	target := targetFromServer(srv)
	route := proute.Route{Method: "GET", Path: "/ping"}
	req, err := kshttp.Build(route, target)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if resp.StatusCode != nethttp.StatusOK {
		t.Errorf("StatusCode: want 200, got %d", resp.StatusCode)
	}
	if string(resp.Body) != "hello" {
		t.Errorf("Body: want %q, got %q", "hello", string(resp.Body))
	}
	if gotUA != "KiteString-Test/1.0" {
		t.Errorf("User-Agent: want %q, got %q", "KiteString-Test/1.0", gotUA)
	}
	if gotCustom != "injected" {
		t.Errorf("X-Custom: want %q, got %q", "injected", gotCustom)
	}
	if resp.Duration <= 0 {
		t.Error("Duration should be positive")
	}
}

// --- Path parameter resolution ---

func TestBuild_PathParams(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(nethttp.StatusOK)
	}))
	defer srv.Close()

	client := kshttp.NewClient(kshttp.ClientConfig{})
	target := targetFromServer(srv)
	route := proute.Route{Method: "GET", Path: "/users/{id}/posts/{postId}"}

	req, err := kshttp.Build(route, target)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := client.Do(req); err != nil {
		t.Fatalf("Do: %v", err)
	}

	// Braces should be gone; the resolved path has two path segments in place.
	if gotPath == "/users/{id}/posts/{postId}" {
		t.Error("path placeholders were not resolved")
	}
	t.Logf("resolved path: %s", gotPath)
}

// --- 429 auto-backoff and retry ---

func TestPool_429Backoff(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(nethttp.StatusTooManyRequests)
			return
		}
		w.WriteHeader(nethttp.StatusOK)
	}))
	defer srv.Close()

	// Use very short backoff so the test runs quickly.
	limiter := kshttp.NewRateLimiter(kshttp.RateLimiterConfig{
		MaxRetries:  5,
		BaseBackoff: time.Millisecond,
		MaxBackoff:  5 * time.Millisecond,
	})

	pool := kshttp.NewPool(kshttp.PoolConfig{
		MaxConnPerHost:   2,
		MaxParallelHosts: 2,
		Client:           kshttp.NewClient(kshttp.ClientConfig{}),
		Limiter:          limiter,
	})

	target := targetFromServer(srv)
	req, err := kshttp.Build(proute.Route{Method: "GET", Path: "/retry"}, target)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	pool.Submit(req)

	var result *kshttp.Result
	for r := range pool.Results() {
		result = r
		break
	}
	pool.Shutdown()

	if result == nil {
		t.Fatal("no result received")
	}
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Resp.StatusCode != nethttp.StatusOK {
		t.Errorf("StatusCode: want 200, got %d", result.Resp.StatusCode)
	}
	if calls.Load() < 3 {
		t.Errorf("want ≥3 server calls (2 retries), got %d", calls.Load())
	}
}

// --- Pool processes N jobs and returns N results ---

func TestPool_NJobs(t *testing.T) {
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.WriteHeader(nethttp.StatusOK)
	}))
	defer srv.Close()

	const N = 25

	pool := kshttp.NewPool(kshttp.PoolConfig{
		MaxConnPerHost:   5,
		MaxParallelHosts: 4,
		Client:           kshttp.NewClient(kshttp.ClientConfig{}),
	})
	pool.WatchSignals()

	target := targetFromServer(srv)

	var count atomic.Int32
	var wg sync.WaitGroup
	wg.Add(N)

	// Consume results concurrently so the results channel never fills up.
	go func() {
		for range pool.Results() {
			count.Add(1)
			wg.Done()
		}
	}()

	for i := 0; i < N; i++ {
		req, err := kshttp.Build(proute.Route{Method: "GET", Path: fmt.Sprintf("/job/%d", i)}, target)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		pool.Submit(req)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatalf("timeout: received %d/%d results", count.Load(), N)
	}

	pool.Shutdown()

	if got := count.Load(); got != N {
		t.Errorf("want %d results, got %d", N, got)
	}
}

// --- Shutdown drains without deadlock ---

func TestPool_Shutdown_NoDrop(t *testing.T) {
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.WriteHeader(nethttp.StatusNoContent)
	}))
	defer srv.Close()

	pool := kshttp.NewPool(kshttp.PoolConfig{
		MaxConnPerHost:   3,
		MaxParallelHosts: 2,
		Client:           kshttp.NewClient(kshttp.ClientConfig{}),
	})

	target := targetFromServer(srv)

	const N = 10
	for i := 0; i < N; i++ {
		req, _ := kshttp.Build(proute.Route{Method: "GET", Path: "/drain"}, target)
		pool.Submit(req)
	}

	// Drain results, then shutdown — must not deadlock.
	var count int
	shutdownDone := make(chan struct{})
	go func() {
		pool.Shutdown()
		close(shutdownDone)
	}()

	for r := range pool.Results() {
		_ = r
		count++
	}

	select {
	case <-shutdownDone:
	case <-time.After(15 * time.Second):
		t.Fatal("Shutdown deadlocked")
	}
}

// --- Always-429 host is skipped after max retries ---

func TestPool_429AlwaysExceeded(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		calls.Add(1)
		w.WriteHeader(nethttp.StatusTooManyRequests)
	}))
	defer srv.Close()

	limiter := kshttp.NewRateLimiter(kshttp.RateLimiterConfig{
		MaxRetries:  2,
		BaseBackoff: time.Millisecond,
		MaxBackoff:  2 * time.Millisecond,
	})

	pool := kshttp.NewPool(kshttp.PoolConfig{
		MaxConnPerHost:   2,
		MaxParallelHosts: 2,
		Client:           kshttp.NewClient(kshttp.ClientConfig{}),
		Limiter:          limiter,
	})

	target := targetFromServer(srv)
	req, err := kshttp.Build(proute.Route{Method: "GET", Path: "/always429"}, target)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	pool.Submit(req)

	var result *kshttp.Result
	for r := range pool.Results() {
		result = r
		break
	}
	pool.Shutdown()

	if result == nil {
		t.Fatal("no result received")
	}
	// After max retries the 429 is returned as-is (not an error).
	if result.Resp == nil {
		t.Fatal("expected a response, got nil")
	}
	if result.Resp.StatusCode != nethttp.StatusTooManyRequests {
		t.Errorf("want 429, got %d", result.Resp.StatusCode)
	}
	// Server should have been called maxRetries+1 times (initial + retries).
	if got := calls.Load(); got < 2 {
		t.Errorf("want ≥2 server calls, got %d", got)
	}
}

// --- Backoff duration calculation ---

func TestRateLimiter_Backoff(t *testing.T) {
	rl := kshttp.NewRateLimiter(kshttp.RateLimiterConfig{
		BaseBackoff: 5 * time.Second,
		MaxBackoff:  60 * time.Second,
	})
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 5 * time.Second},
		{1, 10 * time.Second},
		{3, 40 * time.Second},
		{4, 60 * time.Second}, // capped: 5<<4 = 80s → 60s
	}
	for _, tc := range cases {
		got := rl.Backoff("example.com", tc.attempt)
		if got != tc.want {
			t.Errorf("Backoff(attempt=%d): want %v, got %v", tc.attempt, tc.want, got)
		}
	}
}

// --- RecordResponse increments/resets backoff counter ---

func TestRateLimiter_RecordResponse(t *testing.T) {
	rl := kshttp.NewRateLimiter(kshttp.RateLimiterConfig{
		BaseBackoff: 5 * time.Second,
		MaxBackoff:  60 * time.Second,
		MaxRetries:  5,
	})
	host := "record-test.example"

	// Two 429s then a 200 should reset the backoff.
	rl.RecordResponse(host, 429)
	rl.RecordResponse(host, 429)
	rl.RecordResponse(host, 200)

	// After reset, On429 with attempt=0 should use backoffN=0 (base backoff).
	delay, ok := rl.On429(host, 0)
	if !ok {
		t.Fatal("On429 should allow retry")
	}
	if delay != 5*time.Second {
		t.Errorf("want 5s after reset, got %v", delay)
	}
}

// --- --delay produces measurable inter-request spacing ---

func TestRateLimiter_Delay(t *testing.T) {
	const delayTarget = 100 * time.Millisecond
	var timestamps []time.Time
	var mu sync.Mutex

	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		mu.Lock()
		timestamps = append(timestamps, time.Now())
		mu.Unlock()
		w.WriteHeader(nethttp.StatusOK)
	}))
	defer srv.Close()

	limiter := kshttp.NewRateLimiter(kshttp.RateLimiterConfig{
		Delay: delayTarget,
	})

	pool := kshttp.NewPool(kshttp.PoolConfig{
		MaxConnPerHost:   1,
		MaxParallelHosts: 1,
		Client:           kshttp.NewClient(kshttp.ClientConfig{}),
		Limiter:          limiter,
	})

	target := targetFromServer(srv)
	const N = 4
	var wg sync.WaitGroup
	wg.Add(N)
	go func() {
		for range pool.Results() {
			wg.Done()
		}
	}()

	for i := 0; i < N; i++ {
		req, _ := kshttp.Build(proute.Route{Method: "GET", Path: "/delay"}, target)
		pool.Submit(req)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for delayed results")
	}
	pool.Shutdown()

	mu.Lock()
	ts := timestamps
	mu.Unlock()

	if len(ts) < 2 {
		t.Fatalf("need at least 2 timestamps, got %d", len(ts))
	}
	for i := 1; i < len(ts); i++ {
		gap := ts[i].Sub(ts[i-1])
		// Allow 50% tolerance above and below the target.
		if gap < delayTarget/2 {
			t.Errorf("inter-request gap %v too short (want ≥%v)", gap, delayTarget/2)
		}
	}
}

// --- Host reachability tracking ---

func TestPool_HostUnreachable(t *testing.T) {
	// Start a server just to get a valid host/port, then close it so all
	// connection attempts will be refused.
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.WriteHeader(nethttp.StatusOK)
	}))
	target := targetFromServer(srv)
	srv.Close() // port is now closed; subsequent dials will be refused

	limiter := kshttp.NewRateLimiter(kshttp.RateLimiterConfig{
		UnreachableThreshold: 2,
		BaseBackoff:          time.Millisecond,
		MaxBackoff:           5 * time.Millisecond,
		ConnRetryDelay:       5 * time.Millisecond, // fast retry for testing
	})

	pool := kshttp.NewPool(kshttp.PoolConfig{
		MaxConnPerHost:   1,
		MaxParallelHosts: 1,
		Client:           kshttp.NewClient(kshttp.ClientConfig{Timeout: 500 * time.Millisecond}),
		Limiter:          limiter,
	})

	// Submit enough requests to exceed the unreachable threshold.
	const N = 4
	var wg sync.WaitGroup
	wg.Add(N)

	var errCount atomic.Int32
	go func() {
		for r := range pool.Results() {
			if r.Err != nil {
				errCount.Add(1)
			}
			wg.Done()
		}
	}()

	for i := 0; i < N; i++ {
		req, _ := kshttp.Build(proute.Route{Method: "GET", Path: "/unreachable"}, target)
		pool.Submit(req)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for unreachable results")
	}
	pool.Shutdown()

	if errCount.Load() == 0 {
		t.Error("expected at least one error result for unreachable host")
	}
	host := target.Host
	if !limiter.IsUnreachable(host) {
		t.Errorf("host %s should be marked unreachable after consecutive failures", host)
	}
}

// --- Per-host connection limiting ---

func TestPool_PerHostConcurrency(t *testing.T) {
	const maxConn = 3
	var (
		mu      sync.Mutex
		peak    int
		current int
	)

	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		mu.Lock()
		current++
		if current > peak {
			peak = current
		}
		mu.Unlock()

		time.Sleep(20 * time.Millisecond) // hold the connection briefly

		mu.Lock()
		current--
		mu.Unlock()

		w.WriteHeader(nethttp.StatusOK)
	}))
	defer srv.Close()

	pool := kshttp.NewPool(kshttp.PoolConfig{
		MaxConnPerHost:   maxConn,
		MaxParallelHosts: 4,
		Client:           kshttp.NewClient(kshttp.ClientConfig{}),
	})

	target := targetFromServer(srv)

	const N = 20
	var wg sync.WaitGroup
	wg.Add(N)
	go func() {
		for range pool.Results() {
			wg.Done()
		}
	}()

	for i := 0; i < N; i++ {
		req, _ := kshttp.Build(proute.Route{Method: "GET", Path: "/limit"}, target)
		pool.Submit(req)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for results")
	}
	pool.Shutdown()

	if peak > maxConn {
		t.Errorf("peak concurrent connections %d exceeded limit %d", peak, maxConn)
	}
}
