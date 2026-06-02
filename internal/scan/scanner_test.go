package scan_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RowanDark/kitestring/internal/scan"
	"github.com/RowanDark/kitestring/pkg/proute"
)

// TestScanner_FindsValidRoutes verifies that the scanner:
//   - discovers all 5 valid routes
//   - filters a 404 response via FailStatusCodes
//   - filters a response whose content length falls in IgnoreLengths
func TestScanner_FindsValidRoutes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"users":[]}`)
	})
	mux.HandleFunc("/api/products", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"products":[]}`)
	})
	mux.HandleFunc("/api/orders", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"orders":[]}`)
	})
	mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"token":"abc"}`)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "ok")
	})
	// /filtered-length returns 200 with exactly 42 bytes — should be suppressed.
	mux.HandleFunc("/filtered-length", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		w.Write(bytes.Repeat([]byte("x"), 42)) //nolint:errcheck
	})
	// /not-found is unregistered; http.ServeMux returns 404 automatically.

	srv := httptest.NewServer(mux)
	defer srv.Close()

	targets, err := proute.ParseTarget(srv.URL)
	if err != nil {
		t.Fatalf("ParseTarget: %v", err)
	}

	routes := []proute.Route{
		{Method: "GET", Path: "/api/users"},
		{Method: "GET", Path: "/api/products"},
		{Method: "GET", Path: "/api/orders"},
		{Method: "POST", Path: "/api/auth/login"},
		{Method: "GET", Path: "/health"},
		{Method: "GET", Path: "/not-found"},       // 404 — suppressed by FailStatusCodes
		{Method: "GET", Path: "/filtered-length"}, // 42-byte body — suppressed by IgnoreLengths
	}

	lr, err := proute.ParseLengthRange("42")
	if err != nil {
		t.Fatalf("ParseLengthRange: %v", err)
	}

	config := proute.ScanConfig{
		MaxConnPerHost:   2,
		MaxParallelHosts: 2,
		DisablePreflight: true,
		FailStatusCodes:  []int{404},
		IgnoreLengths:    []proute.LengthRange{lr},
	}

	s, err := scan.New(config)
	if err != nil {
		t.Fatalf("scan.New: %v", err)
	}
	s.SetOutput(io.Discard)

	if err := s.Run(targets, routes); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Expect exactly 5 results: the 404 and the filtered-length response are excluded.
	if got := s.ResultCount(); got != 5 {
		t.Errorf("ResultCount = %d, want 5", got)
	}
}

// TestScanner_FiltersStatusCode verifies that FailStatusCodes suppresses matching responses.
func TestScanner_FiltersStatusCode(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/found", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "found")
	})
	// /not-found handled by default 404 mux handler.

	srv := httptest.NewServer(mux)
	defer srv.Close()

	targets, err := proute.ParseTarget(srv.URL)
	if err != nil {
		t.Fatalf("ParseTarget: %v", err)
	}

	routes := []proute.Route{
		{Method: "GET", Path: "/found"},
		{Method: "GET", Path: "/not-found"},
	}

	config := proute.ScanConfig{
		MaxConnPerHost:   2,
		MaxParallelHosts: 2,
		DisablePreflight: true,
		FailStatusCodes:  []int{404},
	}

	s, err := scan.New(config)
	if err != nil {
		t.Fatalf("scan.New: %v", err)
	}
	s.SetOutput(io.Discard)

	if err := s.Run(targets, routes); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := s.ResultCount(); got != 1 {
		t.Errorf("ResultCount = %d, want 1 (only /found passes)", got)
	}
}

// TestScanner_FiltersIgnoredContentLength verifies that IgnoreLengths suppresses
// responses whose content length falls within any configured range.
func TestScanner_FiltersIgnoredContentLength(t *testing.T) {
	const ignoredLen = 77
	mux := http.NewServeMux()
	mux.HandleFunc("/keep", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "keep this response")
	})
	mux.HandleFunc("/ignored", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		w.Write(bytes.Repeat([]byte("x"), ignoredLen)) //nolint:errcheck
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	targets, err := proute.ParseTarget(srv.URL)
	if err != nil {
		t.Fatalf("ParseTarget: %v", err)
	}

	routes := []proute.Route{
		{Method: "GET", Path: "/keep"},
		{Method: "GET", Path: "/ignored"},
	}

	lr, err := proute.ParseLengthRange(fmt.Sprintf("%d", ignoredLen))
	if err != nil {
		t.Fatalf("ParseLengthRange: %v", err)
	}

	config := proute.ScanConfig{
		MaxConnPerHost:   2,
		MaxParallelHosts: 2,
		DisablePreflight: true,
		IgnoreLengths:    []proute.LengthRange{lr},
	}

	s, err := scan.New(config)
	if err != nil {
		t.Fatalf("scan.New: %v", err)
	}
	s.SetOutput(io.Discard)

	if err := s.Run(targets, routes); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := s.ResultCount(); got != 1 {
		t.Errorf("ResultCount = %d, want 1 (only /keep passes)", got)
	}
}

// TestScanner_QuarantinesWildcardHost verifies that a server returning an
// identical response for all paths is quarantined before all routes are reported.
func TestScanner_QuarantinesWildcardHost(t *testing.T) {
	const wildcardBody = "wildcard response body"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		fmt.Fprint(w, wildcardBody)
	}))
	defer srv.Close()

	targets, err := proute.ParseTarget(srv.URL)
	if err != nil {
		t.Fatalf("ParseTarget: %v", err)
	}

	// 10 routes — quarantine fires after QuarantineThresh (3) wildcard hits.
	routes := make([]proute.Route, 10)
	for i := range routes {
		routes[i] = proute.Route{Method: "GET", Path: fmt.Sprintf("/route%d", i)}
	}

	config := proute.ScanConfig{
		MaxConnPerHost:    2,
		MaxParallelHosts:  2,
		DisablePreflight:  false,
		WildcardDetection: true,
		QuarantineThresh:  3,
		PreflightDepth:    1,
	}

	s, err := scan.New(config)
	if err != nil {
		t.Fatalf("scan.New: %v", err)
	}
	s.SetOutput(io.Discard)

	if err := s.Run(targets, routes); err != nil {
		t.Fatalf("Run: %v", err)
	}

	host := targets[0].Host
	if !s.Quarantine().Check(host) {
		t.Errorf("expected host %s to be quarantined after wildcard detection, but it was not", host)
	}

	// All responses matched the wildcard baseline, so no results should be emitted.
	if got := s.ResultCount(); got != 0 {
		t.Errorf("ResultCount = %d, want 0 (all responses were wildcards)", got)
	}
}
