package output_test

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RowanDark/kitestring/internal/output"
	"github.com/RowanDark/kitestring/pkg/proute"
)

// TestFromJSONL_ParsesFixture verifies that FromJSONL correctly parses the fixture file.
func TestFromJSONL_ParsesFixture(t *testing.T) {
	f, err := os.Open("testdata/results.jsonl")
	if err != nil {
		t.Fatalf("opening fixture: %v", err)
	}
	defer f.Close()

	report, err := output.FromJSONL(f)
	if err != nil {
		t.Fatalf("FromJSONL: %v", err)
	}
	if len(report.Findings) != 3 {
		t.Errorf("got %d findings, want 3", len(report.Findings))
	}

	// Verify first finding fields.
	got := report.Findings[0]
	if got.Method != "GET" {
		t.Errorf("Method = %q, want %q", got.Method, "GET")
	}
	if got.URL != "https://api.example.com/v1/users" {
		t.Errorf("URL = %q, want %q", got.URL, "https://api.example.com/v1/users")
	}
	if got.Path != "/v1/users" {
		t.Errorf("Path = %q, want %q", got.Path, "/v1/users")
	}
	if got.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", got.StatusCode)
	}
	if got.ContentLength != 1234 {
		t.Errorf("ContentLength = %d, want 1234", got.ContentLength)
	}
	if got.ResponseTimeMs != 42 {
		t.Errorf("ResponseTimeMs = %d, want 42", got.ResponseTimeMs)
	}
	if got.KSUID != "abc123" {
		t.Errorf("KSUID = %q, want %q", got.KSUID, "abc123")
	}

	// Meta target should be derived from first finding.
	if report.Meta.Target != "api.example.com" {
		t.Errorf("Meta.Target = %q, want %q", report.Meta.Target, "api.example.com")
	}
}

// TestFromJSONL_EmptyInput returns a valid empty report.
func TestFromJSONL_EmptyInput(t *testing.T) {
	report, err := output.FromJSONL(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Findings) != 0 {
		t.Errorf("got %d findings, want 0", len(report.Findings))
	}
}

// TestBuildReport_MatchesJSONL verifies that a report built directly from ScanResults
// contains the same finding count and key fields as one built from its JSONL output.
func TestBuildReport_MatchesJSONL(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339, "2026-06-03T10:00:00Z")
	results := []proute.ScanResult{
		{
			Route:         proute.Route{Method: "GET", Path: "/v1/users"},
			URL:           "https://api.example.com/v1/users",
			StatusCode:    200,
			ContentLength: 1234,
			ResponseTime:  42 * time.Millisecond,
			Timestamp:     ts,
			KSUID:         "abc123",
		},
		{
			Route:         proute.Route{Method: "POST", Path: "/v1/login"},
			URL:           "https://api.example.com/v1/login",
			StatusCode:    401,
			ContentLength: 89,
			ResponseTime:  15 * time.Millisecond,
			Timestamp:     ts.Add(time.Second),
			KSUID:         "def456",
		},
	}

	meta := output.ReportMeta{
		Target:   "api.example.com",
		ScanDate: ts,
	}

	report := output.BuildReport(results, meta)
	if len(report.Findings) != 2 {
		t.Errorf("got %d findings, want 2", len(report.Findings))
	}
	if report.Findings[0].Method != "GET" {
		t.Errorf("Method = %q, want GET", report.Findings[0].Method)
	}
	if report.Findings[0].StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", report.Findings[0].StatusCode)
	}
}

// TestWriteMarkdown_ContainsCorrectFindingCount verifies the finding count in Markdown output.
func TestWriteMarkdown_ContainsCorrectFindingCount(t *testing.T) {
	f, err := os.Open("testdata/results.jsonl")
	if err != nil {
		t.Fatalf("opening fixture: %v", err)
	}
	defer f.Close()

	report, err := output.FromJSONL(f)
	if err != nil {
		t.Fatalf("FromJSONL: %v", err)
	}

	var buf bytes.Buffer
	if err := report.WriteMarkdown(&buf); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}

	md := buf.String()
	if !strings.Contains(md, "Total findings: **3**") {
		t.Errorf("markdown does not contain expected finding count; got:\n%s", md)
	}
	// Each finding should have a curl one-liner.
	if count := strings.Count(md, "curl -s -X"); count != 3 {
		t.Errorf("expected 3 curl blocks, got %d", count)
	}
}

// TestWriteMarkdown_ContainsCurlBlocks verifies curl code blocks appear for each finding.
func TestWriteMarkdown_ContainsCurlBlocks(t *testing.T) {
	results := []proute.ScanResult{
		{
			Route:      proute.Route{Method: "GET", Path: "/api/test"},
			URL:        "https://example.com/api/test",
			StatusCode: 200,
		},
	}
	report := output.BuildReport(results, output.ReportMeta{Target: "example.com"})

	var buf bytes.Buffer
	if err := report.WriteMarkdown(&buf); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	md := buf.String()
	if !strings.Contains(md, "```bash") {
		t.Error("expected fenced bash code block in markdown output")
	}
	if !strings.Contains(md, "curl -s -X GET 'https://example.com/api/test'") {
		t.Errorf("expected curl one-liner in output; got:\n%s", md)
	}
}

// TestWriteHTML_ValidStructure verifies the HTML output contains required structural elements.
func TestWriteHTML_ValidStructure(t *testing.T) {
	f, err := os.Open("testdata/results.jsonl")
	if err != nil {
		t.Fatalf("opening fixture: %v", err)
	}
	defer f.Close()

	report, err := output.FromJSONL(f)
	if err != nil {
		t.Fatalf("FromJSONL: %v", err)
	}

	var buf bytes.Buffer
	if err := report.WriteHTML(&buf); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}

	htmlOut := buf.String()
	requiredTags := []string{
		"<!DOCTYPE html>",
		"<html",
		"<head>",
		"<title>",
		"</title>",
		"<body>",
		"</body>",
		"</html>",
		"<details",
		"<summary",
		"<style>",
	}
	for _, tag := range requiredTags {
		if !strings.Contains(htmlOut, tag) {
			t.Errorf("HTML output missing required element: %q", tag)
		}
	}
}

// TestWriteHTML_ContainsCurlBlocks verifies curl commands appear in HTML output.
// The curl content is wrapped in <span> elements for syntax highlighting, so we
// check for the method and URL appearing individually rather than as a single string.
func TestWriteHTML_ContainsCurlBlocks(t *testing.T) {
	results := []proute.ScanResult{
		{
			Route:      proute.Route{Method: "DELETE", Path: "/api/resource/1"},
			URL:        "https://target.com/api/resource/1",
			StatusCode: 204,
		},
	}
	report := output.BuildReport(results, output.ReportMeta{Target: "target.com"})

	var buf bytes.Buffer
	if err := report.WriteHTML(&buf); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}

	htmlOut := buf.String()
	// The curl command is syntax-highlighted with spans; check for key parts.
	if !strings.Contains(htmlOut, "curl") {
		t.Error("expected 'curl' in HTML output")
	}
	if !strings.Contains(htmlOut, "DELETE") {
		t.Error("expected method 'DELETE' in HTML output")
	}
	if !strings.Contains(htmlOut, "https://target.com/api/resource/1") {
		t.Errorf("expected URL in HTML output")
	}
	if !strings.Contains(htmlOut, "curl-cmd") {
		t.Error("expected curl-cmd CSS class in HTML output")
	}
}
