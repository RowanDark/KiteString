package scan

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RowanDark/kitestring/pkg/proute"
)

// targetFromServer parses a test server URL into a ScanTarget.
func targetFromServer(t *testing.T, srv *httptest.Server) proute.ScanTarget {
	t.Helper()
	targets, err := proute.ParseTarget(srv.URL)
	if err != nil {
		t.Fatalf("ParseTarget(%q): %v", srv.URL, err)
	}
	return targets[0]
}

func TestCheckHost_Reachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer srv.Close()

	target := targetFromServer(t, srv)
	profile, err := CheckHost(target, srv.Client())
	if err != nil {
		t.Fatalf("CheckHost: %v", err)
	}
	if profile.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", profile.StatusCode)
	}
}

func TestCheckHost_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	// Close the server before making the request so it's unreachable.
	srv.Close()

	target := targetFromServer(t, srv)
	_, err := CheckHost(target, srv.Client())
	if err == nil {
		t.Error("expected error for unreachable host, got nil")
	}
}

// TestBuildBaselines_WildcardHost verifies that a server returning the same
// response for every path produces a baseline that matches those responses.
func TestBuildBaselines_WildcardHost(t *testing.T) {
	const wildcardBody = "<html><body>wildcard page</body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, wildcardBody)
	}))
	defer srv.Close()

	target := targetFromServer(t, srv)
	routes := []proute.Route{{Method: "GET", Path: "/api/users"}}

	baselines, err := BuildBaselines(target, routes, 0, srv.Client())
	if err != nil {
		t.Fatalf("BuildBaselines: %v", err)
	}

	baseline, ok := baselines["/"]
	if !ok {
		t.Fatal("expected baseline entry for /")
	}
	if baseline.StatusCode != http.StatusOK {
		t.Errorf("baseline status = %d, want 200", baseline.StatusCode)
	}
	wantHash := sha256.Sum256([]byte(wildcardBody))
	if baseline.BodyHash != wantHash {
		t.Error("baseline body hash does not match expected wildcard body")
	}

	// A real request to any path should be identified as a wildcard match.
	wcReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/api/users", http.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := srv.Client().Do(wcReq)
	if err != nil {
		t.Fatalf("Get /api/users: %v", err)
	}
	if !IsWildcard(resp, baseline) {
		t.Error("expected IsWildcard=true for wildcard host, got false")
	}
}

// TestBuildBaselines_SpecificRoutes verifies that a server with real routes
// is not falsely identified as a wildcard.
func TestBuildBaselines_SpecificRoutes(t *testing.T) {
	const validBody = `{"users":[]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/users" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, validBody)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	target := targetFromServer(t, srv)
	routes := []proute.Route{{Method: "GET", Path: "/api/users"}}

	// depth 1 produces a baseline for "/api" by probing "/api/<random>" → 404.
	baselines, err := BuildBaselines(target, routes, 1, srv.Client())
	if err != nil {
		t.Fatalf("BuildBaselines: %v", err)
	}

	baseline, ok := baselines["/api"]
	if !ok {
		t.Fatal("expected baseline entry for /api")
	}
	if baseline.StatusCode != http.StatusNotFound {
		t.Errorf("baseline status = %d, want 404", baseline.StatusCode)
	}

	// A request to the real route returns 200 — must NOT be flagged as wildcard.
	specReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/api/users", http.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := srv.Client().Do(specReq)
	if err != nil {
		t.Fatalf("Get /api/users: %v", err)
	}
	if IsWildcard(resp, baseline) {
		t.Error("expected IsWildcard=false for specific valid route, got true")
	}
}

func TestIsWildcard_Match(t *testing.T) {
	body := "<html>wildcard</html>"
	baseline := &Baseline{
		StatusCode:    http.StatusOK,
		ContentLength: int64(len(body)),
		ContentType:   "text/html",
		BodyHash:      sha256.Sum256([]byte(body)),
	}

	resp := &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: int64(len(body)),
		Header:        http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:          io.NopCloser(strings.NewReader(body)),
	}

	if !IsWildcard(resp, baseline) {
		t.Error("expected IsWildcard=true for matching response")
	}
}

func TestIsWildcard_StatusMismatch(t *testing.T) {
	body := `{"error":"not found"}`
	baseline := &Baseline{
		StatusCode:    http.StatusNotFound,
		ContentLength: int64(len(body)),
		ContentType:   "application/json",
		BodyHash:      sha256.Sum256([]byte(body)),
	}

	resp := &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: int64(len(body)),
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(strings.NewReader(body)),
	}

	if IsWildcard(resp, baseline) {
		t.Error("expected IsWildcard=false for status code mismatch")
	}
}

func TestIsWildcard_BodyMismatch(t *testing.T) {
	baselineBody := "<html>wildcard</html>"
	realBody := "<html>real page with unique content</html>"

	baseline := &Baseline{
		StatusCode:    http.StatusOK,
		ContentLength: int64(len(baselineBody)),
		ContentType:   "text/html",
		BodyHash:      sha256.Sum256([]byte(baselineBody)),
	}

	resp := &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: int64(len(realBody)),
		Header:        http.Header{"Content-Type": []string{"text/html"}},
		Body:          io.NopCloser(strings.NewReader(realBody)),
	}

	if IsWildcard(resp, baseline) {
		t.Error("expected IsWildcard=false for body mismatch")
	}
}

// TestPreflight_DisablePrecheck verifies that disable=true skips all HTTP probing.
func TestPreflight_DisablePrecheck(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	target := targetFromServer(t, srv)
	profile, baselines, err := Preflight(target, nil, 1, true, srv.Client())
	if err != nil {
		t.Fatalf("Preflight (disabled): %v", err)
	}
	if called {
		t.Error("server was called but should have been skipped with disable=true")
	}
	if profile != nil {
		t.Error("profile should be nil when preflight is disabled")
	}
	if baselines != nil {
		t.Error("baselines should be nil when preflight is disabled")
	}
}

func TestPrefixAtDepth(t *testing.T) {
	cases := []struct {
		path  string
		depth int
		want  string
	}{
		{"/api/v1/users", 0, "/"},
		{"/api/v1/users", 1, "/api"},
		{"/api/v1/users", 2, "/api/v1"},
		{"/api/v1/users", 3, "/api/v1/users"},
		{"/api", 1, "/api"},
		{"/", 1, "/"},
		{"", 1, "/"},
	}
	for _, tc := range cases {
		got := prefixAtDepth(tc.path, tc.depth)
		if got != tc.want {
			t.Errorf("prefixAtDepth(%q, %d) = %q, want %q", tc.path, tc.depth, got, tc.want)
		}
	}
}
