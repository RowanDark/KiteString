package proute

import (
	"strings"
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

func TestParseInputLineBlank(t *testing.T) {
	got, err := ParseInputLine("")
	if err != nil || got != nil {
		t.Errorf("blank line: expected nil, nil; got %v, %v", got, err)
	}
}

func TestParseInputLineWhitespace(t *testing.T) {
	got, err := ParseInputLine("   ")
	if err != nil || got != nil {
		t.Errorf("whitespace line: expected nil, nil; got %v, %v", got, err)
	}
}

func TestParseInputLineComment(t *testing.T) {
	got, err := ParseInputLine("# this is a comment")
	if err != nil || got != nil {
		t.Errorf("comment line: expected nil, nil; got %v, %v", got, err)
	}
}

func TestParseInputLinePlainURL(t *testing.T) {
	got, err := ParseInputLine("https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected target, got nil")
	}
	assertTarget(t, *got, "https", "example.com", 443, "/")
}

func TestParseInputLinePlainHost(t *testing.T) {
	got, err := ParseInputLine("example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected target, got nil")
	}
	assertTarget(t, *got, "http", "example.com", 80, "/")
}

func TestParseInputLineHTTPXStandard(t *testing.T) {
	line := "https://example.com [200] [Example Title] [nginx,php,jQuery]"
	got, err := ParseInputLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected target, got nil")
	}
	assertTarget(t, *got, "https", "example.com", 443, "/")
	if len(got.Tags) != 3 || got.Tags[0] != "nginx" || got.Tags[1] != "php" || got.Tags[2] != "jQuery" {
		t.Errorf("expected tags [nginx php jQuery], got %v", got.Tags)
	}
}

func TestParseInputLineHTTPXStandardNoTech(t *testing.T) {
	// Only status + title, no tech bracket — no tags should be extracted.
	line := "https://example.com [200] [Some Title]"
	got, err := ParseInputLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected target, got nil")
	}
	assertTarget(t, *got, "https", "example.com", 443, "/")
	if len(got.Tags) != 0 {
		t.Errorf("expected no tags, got %v", got.Tags)
	}
}

func TestParseInputLineHTTPXJSON(t *testing.T) {
	line := `{"url":"https://example.com","status_code":200,"title":"Example","tech":["nginx","php"]}`
	got, err := ParseInputLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected target, got nil")
	}
	assertTarget(t, *got, "https", "example.com", 443, "/")
	if len(got.Tags) != 2 || got.Tags[0] != "nginx" || got.Tags[1] != "php" {
		t.Errorf("expected tags [nginx php], got %v", got.Tags)
	}
}

func TestParseInputLineHTTPXJSONNoTech(t *testing.T) {
	line := `{"url":"https://example.com","status_code":200,"title":"Example"}`
	got, err := ParseInputLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected target, got nil")
	}
	assertTarget(t, *got, "https", "example.com", 443, "/")
	if len(got.Tags) != 0 {
		t.Errorf("expected no tags, got %v", got.Tags)
	}
}

func TestParseInputStream(t *testing.T) {
	data := "# comment line\n" +
		"https://alpha.com [200] [Alpha Title] [apache,python]\n" +
		`{"url":"https://beta.com","status_code":200,"tech":["nginx"]}` + "\n" +
		"https://gamma.com\n" +
		"delta.com\n" +
		"\n" +
		"   \n"

	targets, err := ParseInputStream(strings.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 4 {
		t.Errorf("expected 4 targets, got %d", len(targets))
	}
}

func TestParseInputStreamEmpty(t *testing.T) {
	targets, err := ParseInputStream(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("expected 0 targets, got %d", len(targets))
	}
}

func TestParseInputStreamOnlyComments(t *testing.T) {
	data := "# first comment\n# second comment\n"
	targets, err := ParseInputStream(strings.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("expected 0 targets, got %d", len(targets))
	}
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
