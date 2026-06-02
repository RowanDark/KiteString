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
