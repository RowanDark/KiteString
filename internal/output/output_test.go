package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RowanDark/kitestring/pkg/proute"
)

func makeScanResult(method, url string, status, length int) proute.ScanResult {
	return proute.ScanResult{
		Target: proute.ScanTarget{Scheme: "https", Host: "target.com"},
		Route: proute.Route{
			Method: method,
			Path:   "/api/v1/users",
			Source: "apiroutes.ks",
			KSUID:  "a1b2c3d4e5f6",
		},
		StatusCode:    status,
		ContentLength: length,
		ResponseTime:  142 * time.Millisecond,
		Timestamp:     time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		URL:           url,
		KSUID:         "a1b2c3d4e5f6",
	}
}

func makePostResult() proute.ScanResult {
	r := makeScanResult("POST", "https://target.com/api/v1/users", 200, 1337)
	r.Route.ContentType = "application/json"
	r.Route.BodyParams = []proute.Crumb{
		{Key: "email", Type: proute.CrumbEmail, Example: "test@example.com"},
		{Key: "name", Type: proute.CrumbString, Example: "Alice"},
	}
	return r
}

// --- JSONL tests ---

func TestJSONLValidLines(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter("jsonl", &buf)
	if err != nil {
		t.Fatal(err)
	}

	results := []proute.ScanResult{
		makeScanResult("GET", "https://target.com/api/v1/users", 200, 500),
		makePostResult(),
		makeScanResult("DELETE", "https://target.com/api/v1/users/1", 204, 0),
	}

	for _, r := range results {
		if err := w.WriteResult(r); err != nil {
			t.Fatalf("WriteResult: %v", err)
		}
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != len(results) {
		t.Fatalf("expected %d lines, got %d", len(results), len(lines))
	}

	for i, line := range lines {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("line %d not valid JSON: %v\nline: %s", i, err, line)
		}
		for _, field := range []string{"method", "url", "status", "content_length", "response_time_ms", "timestamp", "curl"} {
			if _, ok := obj[field]; !ok {
				t.Errorf("line %d missing field %q", i, field)
			}
		}
	}
}

func TestJSONLResultFields(t *testing.T) {
	var buf bytes.Buffer
	w, _ := NewWriter("jsonl", &buf)

	r := makePostResult()
	_ = w.WriteResult(r)

	var obj map[string]interface{}
	if err := json.Unmarshal(buf.Bytes()[:len(buf.Bytes())-1], &obj); err != nil {
		t.Fatal(err)
	}

	if got := obj["method"]; got != "POST" {
		t.Errorf("method = %v, want POST", got)
	}
	if got := obj["status"]; got != float64(200) {
		t.Errorf("status = %v, want 200", got)
	}
	if got := obj["source"]; got != "apiroutes.ks" {
		t.Errorf("source = %v, want apiroutes.ks", got)
	}
	if curl, ok := obj["curl"].(string); !ok || !strings.HasPrefix(curl, "curl ") {
		t.Errorf("curl field missing or malformed: %v", obj["curl"])
	}
}

func TestJSONLSummary(t *testing.T) {
	var buf bytes.Buffer
	w, _ := NewWriter("jsonl", &buf)

	s := ScanSummary{
		TotalTargets: 3,
		TotalRoutes:  50,
		TotalResults: 12,
		Duration:     5 * time.Second,
	}
	if err := w.WriteSummary(s); err != nil {
		t.Fatal(err)
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &obj); err != nil {
		t.Fatalf("summary not valid JSON: %v", err)
	}
	if obj["type"] != "summary" {
		t.Errorf("type = %v, want summary", obj["type"])
	}
	if obj["total_results"] != float64(12) {
		t.Errorf("total_results = %v, want 12", obj["total_results"])
	}
}

// --- Curl tests ---

func TestCurlGETNoBody(t *testing.T) {
	r := makeScanResult("GET", "https://target.com/api/v1/users?limit=10", 200, 500)
	curl := GenerateCurl(r)

	if !strings.HasPrefix(curl, "curl -X GET ") {
		t.Errorf("unexpected prefix: %s", curl)
	}
	if !strings.Contains(curl, "https://target.com/api/v1/users?limit=10") {
		t.Errorf("URL not in curl: %s", curl)
	}
	if strings.Contains(curl, " -d ") {
		t.Errorf("GET should not have -d flag: %s", curl)
	}
}

func TestCurlPOSTWithBody(t *testing.T) {
	r := makePostResult()
	curl := GenerateCurl(r)

	if !strings.HasPrefix(curl, "curl -X POST ") {
		t.Errorf("unexpected prefix: %s", curl)
	}
	if !strings.Contains(curl, "-H 'Content-Type: application/json'") {
		t.Errorf("Content-Type header missing: %s", curl)
	}
	if !strings.Contains(curl, "-d '") {
		t.Errorf("-d flag missing: %s", curl)
	}

	// Extract the body JSON from the curl command
	idx := strings.Index(curl, "-d '")
	if idx == -1 {
		t.Fatal("cannot find -d in curl output")
	}
	bodyPart := curl[idx+4:]
	// Strip trailing single quote
	bodyPart = strings.TrimSuffix(bodyPart, "'")

	var body map[string]string
	if err := json.Unmarshal([]byte(bodyPart), &body); err != nil {
		t.Errorf("body is not valid JSON: %v\nbody: %s", err, bodyPart)
	}
	if body["email"] != "test@example.com" {
		t.Errorf("email = %q, want test@example.com", body["email"])
	}
}

func TestCurlShellQuote(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"hello", "'hello'"},
		{"it's", `'it'\''s'`},
		{"no quotes", "'no quotes'"},
	}
	for _, c := range cases {
		got := shellQuote(c.in)
		if got != c.want {
			t.Errorf("shellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- Pretty tests ---

func TestPrettyNoColorForNonTTY(t *testing.T) {
	var buf bytes.Buffer
	// bytes.Buffer is not *os.File, so isTTY returns false
	w, err := NewWriter("pretty", &buf)
	if err != nil {
		t.Fatal(err)
	}

	_ = w.WriteResult(makeScanResult("GET", "https://target.com/api/v1/users", 200, 100))

	out := buf.String()
	escapes := []string{colorGreen, colorYellow, colorRed, colorReset, colorBold, colorDim}
	for _, esc := range escapes {
		if strings.Contains(out, esc) {
			t.Errorf("color escape code found in non-TTY output: %q", esc)
		}
	}
}

func TestPrettyContainsKeyFields(t *testing.T) {
	var buf bytes.Buffer
	w, _ := NewWriter("pretty", &buf)

	r := makeScanResult("POST", "https://target.com/api/v1/users", 201, 800)
	_ = w.WriteResult(r)

	out := buf.String()
	for _, want := range []string{"POST", "201", "https://target.com/api/v1/users", "curl"} {
		if !strings.Contains(out, want) {
			t.Errorf("pretty output missing %q\noutput: %s", want, out)
		}
	}
}

// --- Text tests ---

func TestTextNoColorCodes(t *testing.T) {
	var buf bytes.Buffer
	w, _ := NewWriter("text", &buf)

	_ = w.WriteResult(makeScanResult("GET", "https://target.com/health", 200, 50))

	out := buf.String()
	if strings.Contains(out, "\033[") {
		t.Errorf("text output contains ANSI escape codes: %q", out)
	}
}

func TestTextSummaryFormat(t *testing.T) {
	var buf bytes.Buffer
	w, _ := NewWriter("text", &buf)

	s := ScanSummary{TotalTargets: 2, TotalRoutes: 10, TotalResults: 5, Duration: 3 * time.Second}
	_ = w.WriteSummary(s)

	out := buf.String()
	if !strings.HasPrefix(out, "# SUMMARY:") {
		t.Errorf("text summary should start with '# SUMMARY:', got: %s", out)
	}
	if !strings.Contains(out, "5 results") {
		t.Errorf("summary missing result count: %s", out)
	}
}

// --- Summary count tests ---

func TestSummaryCountsMatchResults(t *testing.T) {
	var buf bytes.Buffer
	w, _ := NewWriter("text", &buf)

	results := []proute.ScanResult{
		makeScanResult("GET", "https://a.com/1", 200, 100),
		makeScanResult("POST", "https://a.com/2", 201, 200),
		makeScanResult("GET", "https://b.com/1", 200, 300),
	}

	for _, r := range results {
		_ = w.WriteResult(r)
	}

	s := ScanSummary{
		TotalTargets: 2,
		TotalRoutes:  5,
		TotalResults: len(results),
		Duration:     1 * time.Second,
	}
	_ = w.WriteSummary(s)

	out := buf.String()
	if !strings.Contains(out, "3 results") {
		t.Errorf("summary result count mismatch: %s", out)
	}
}

// --- NewWriter tests ---

func TestNewWriterUnknownFormatDefaultsToPretty(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter("unknown", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := w.(*prettyWriter); !ok {
		t.Errorf("expected *prettyWriter, got %T", w)
	}
}

func TestNewWriterFormats(t *testing.T) {
	cases := []struct {
		format  string
		wantType interface{}
	}{
		{"jsonl", &jsonlWriter{}},
		{"text", &textWriter{}},
		{"pretty", &prettyWriter{}},
		{"", &prettyWriter{}},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		w, err := NewWriter(c.format, &buf)
		if err != nil {
			t.Errorf("NewWriter(%q): %v", c.format, err)
			continue
		}
		_ = w
	}
}

func TestFlushIsNoop(t *testing.T) {
	for _, format := range []string{"pretty", "text", "jsonl"} {
		var buf bytes.Buffer
		w, _ := NewWriter(format, &buf)
		if err := w.Flush(); err != nil {
			t.Errorf("Flush() for %s returned error: %v", format, err)
		}
	}
}
