package proute

import (
	"testing"
)

func TestParseTargetBareDomain(t *testing.T) {
	targets, err := ParseTarget("example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
	assertTarget(t, targets[0], "http", "example.com", 80, "/")
	assertTarget(t, targets[1], "https", "example.com", 443, "/")
}

func TestParseTargetHTTPS(t *testing.T) {
	targets, err := ParseTarget("https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	assertTarget(t, targets[0], "https", "example.com", 443, "/")
}

func TestParseTargetHTTP(t *testing.T) {
	targets, err := ParseTarget("http://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	assertTarget(t, targets[0], "http", "example.com", 80, "/")
}

func TestParseTargetWithPort(t *testing.T) {
	targets, err := ParseTarget("https://example.com:8443/api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	assertTarget(t, targets[0], "https", "example.com", 8443, "/api")
}

func TestParseTargetBareWithPort(t *testing.T) {
	targets, err := ParseTarget("example.com:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	assertTarget(t, targets[0], "http", "example.com", 8080, "/")
}

func TestParseTargetBareWithPath(t *testing.T) {
	targets, err := ParseTarget("example.com/api/v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
	assertTarget(t, targets[0], "http", "example.com", 80, "/api/v1")
	assertTarget(t, targets[1], "https", "example.com", 443, "/api/v1")
}

func TestParseTargetHTTPExplicitPort(t *testing.T) {
	targets, err := ParseTarget("http://example.com:9090/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	assertTarget(t, targets[0], "http", "example.com", 9090, "/path")
}

func TestParseTargetEmpty(t *testing.T) {
	_, err := ParseTarget("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseTargetBareWithSSLPort(t *testing.T) {
	targets, err := ParseTarget("example.com:443")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	assertTarget(t, targets[0], "https", "example.com", 443, "/")
}

func assertTarget(t *testing.T, tgt ScanTarget, scheme, host string, port int, basePath string) {
	t.Helper()
	if tgt.Scheme != scheme {
		t.Errorf("scheme: got %q, want %q", tgt.Scheme, scheme)
	}
	if tgt.Host != host {
		t.Errorf("host: got %q, want %q", tgt.Host, host)
	}
	if tgt.Port != port {
		t.Errorf("port: got %d, want %d", tgt.Port, port)
	}
	if tgt.BasePath != basePath {
		t.Errorf("basePath: got %q, want %q", tgt.BasePath, basePath)
	}
}
