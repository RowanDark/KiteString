package output

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RowanDark/kitestring/pkg/proute"
)

// testMeta returns a minimal ReportMeta suitable for tests.
func testMeta() ReportMeta {
	return ReportMeta{
		Target:    "https://target.com",
		ScanDate:  time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		KSVersion: "dev",
		Wordlists: []string{"routes.ks"},
	}
}

// TestFromJSONLParsesFindings verifies that FromJSONL reconstructs findings from JSONL output.
func TestFromJSONLParsesFindings(t *testing.T) {
	var buf bytes.Buffer
	w, _ := NewWriter("jsonl", &buf)
	results := []proute.ScanResult{
		makeScanResult("GET", "https://target.com/api/users", 200, 512),
		makeScanResult("POST", "https://target.com/api/users", 201, 128),
		makeScanResult("GET", "https://target.com/api/admin", 403, 64),
	}
	for _, r := range results {
		_ = w.WriteResult(r)
	}
	_ = w.WriteSummary(ScanSummary{TotalResults: 3, Duration: 5 * time.Second})

	report, err := FromJSONL(&buf)
	if err != nil {
		t.Fatalf("FromJSONL: %v", err)
	}
	if len(report.Findings) != 3 {
		t.Errorf("findings count: got %d, want 3", len(report.Findings))
	}
	if report.Meta.Duration != 5*time.Second {
		t.Errorf("duration: got %v, want 5s", report.Meta.Duration)
	}

	f := report.Findings[0]
	if f.Method != "GET" {
		t.Errorf("method: got %q, want GET", f.Method)
	}
	if f.StatusCode != 200 {
		t.Errorf("status: got %d, want 200", f.StatusCode)
	}
	if f.URL != "https://target.com/api/users" {
		t.Errorf("url: got %q", f.URL)
	}
	if !strings.HasPrefix(f.Curl, "curl ") {
		t.Errorf("curl field malformed: %q", f.Curl)
	}
	if f.Notes == "" {
		t.Error("notes placeholder should not be empty")
	}
}

// TestFromJSONLSkipsErrorLines verifies that error-type lines are silently skipped.
func TestFromJSONLSkipsErrorLines(t *testing.T) {
	var buf bytes.Buffer
	w, _ := NewWriter("jsonl", &buf)
	_ = w.WriteResult(makeScanResult("GET", "https://target.com/health", 200, 50))
	_ = w.WriteError(fmt.Errorf("connect refused"), "https://target.com")
	_ = w.WriteResult(makeScanResult("GET", "https://target.com/api", 200, 100))

	report, err := FromJSONL(&buf)
	if err != nil {
		t.Fatalf("FromJSONL: %v", err)
	}
	if len(report.Findings) != 2 {
		t.Errorf("expected 2 findings (error skipped), got %d", len(report.Findings))
	}
}

// TestMarkdownFindingCount verifies the finding count appears correctly in markdown.
func TestMarkdownFindingCount(t *testing.T) {
	results := []proute.ScanResult{
		makeScanResult("GET", "https://target.com/api/users", 200, 512),
		makeScanResult("POST", "https://target.com/api/login", 200, 256),
	}
	report := BuildReport(results, testMeta())

	var buf bytes.Buffer
	if err := report.WriteMarkdown(&buf); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "Total findings: **2**") {
		t.Errorf("missing finding count\n%s", out)
	}
	if !strings.Contains(out, "GET") {
		t.Error("missing GET method")
	}
	if !strings.Contains(out, "```bash") {
		t.Error("missing curl code block")
	}
	if !strings.Contains(out, "Add analysis notes here") {
		t.Error("missing notes placeholder")
	}
}

// TestMarkdownStatusTable verifies the executive summary table lists status codes.
func TestMarkdownStatusTable(t *testing.T) {
	results := []proute.ScanResult{
		makeScanResult("GET", "https://target.com/a", 200, 100),
		makeScanResult("GET", "https://target.com/b", 200, 100),
		makeScanResult("GET", "https://target.com/c", 403, 50),
	}
	report := BuildReport(results, testMeta())

	var buf bytes.Buffer
	_ = report.WriteMarkdown(&buf)
	out := buf.String()

	if !strings.Contains(out, "200") {
		t.Error("missing 200 in summary table")
	}
	if !strings.Contains(out, "403") {
		t.Error("missing 403 in summary table")
	}
}

// TestHTMLValidStructure verifies the HTML output contains required structural tags.
func TestHTMLValidStructure(t *testing.T) {
	results := []proute.ScanResult{
		makeScanResult("GET", "https://target.com/api/users", 200, 512),
		makeScanResult("POST", "https://target.com/api/login", 200, 256),
	}
	report := BuildReport(results, testMeta())

	var buf bytes.Buffer
	if err := report.WriteHTML(&buf); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	out := buf.String()

	required := []string{
		"<!DOCTYPE html>",
		"<html",
		"<head>",
		"<body>",
		"</html>",
		"<details",
		"<summary",
		"KiteString Scan Report",
		"<style>",
	}
	for _, tag := range required {
		if !strings.Contains(out, tag) {
			t.Errorf("HTML output missing %q", tag)
		}
	}
	if strings.Contains(out, "<script") {
		t.Error("HTML output must not contain JavaScript")
	}
}

// TestHTMLEscapesSpecialChars verifies that special HTML characters in results are escaped.
func TestHTMLEscapesSpecialChars(t *testing.T) {
	r := makeScanResult("GET", "https://target.com/api?q=<script>", 200, 100)
	report := BuildReport([]proute.ScanResult{r}, testMeta())

	var buf bytes.Buffer
	_ = report.WriteHTML(&buf)
	out := buf.String()

	if strings.Contains(out, "<script>") {
		t.Error("HTML output contains unescaped <script> tag")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Error("HTML output should contain escaped &lt;script&gt;")
	}
}

// TestBuildReportMatchesFromJSONL verifies that a direct BuildReport and a FromJSONL
// round-trip produce the same findings.
func TestBuildReportMatchesFromJSONL(t *testing.T) {
	results := []proute.ScanResult{
		makeScanResult("GET", "https://target.com/api/users", 200, 512),
		makePostResult(),
	}
	direct := BuildReport(results, testMeta())

	var buf bytes.Buffer
	w, _ := NewWriter("jsonl", &buf)
	for _, r := range results {
		_ = w.WriteResult(r)
	}

	fromJSONL, err := FromJSONL(&buf)
	if err != nil {
		t.Fatalf("FromJSONL: %v", err)
	}

	if len(direct.Findings) != len(fromJSONL.Findings) {
		t.Fatalf("finding count: direct=%d, fromJSONL=%d",
			len(direct.Findings), len(fromJSONL.Findings))
	}
	for i := range direct.Findings {
		d, j := direct.Findings[i], fromJSONL.Findings[i]
		if d.Method != j.Method {
			t.Errorf("[%d] Method: direct=%q fromJSONL=%q", i, d.Method, j.Method)
		}
		if d.URL != j.URL {
			t.Errorf("[%d] URL: direct=%q fromJSONL=%q", i, d.URL, j.URL)
		}
		if d.StatusCode != j.StatusCode {
			t.Errorf("[%d] StatusCode: direct=%d fromJSONL=%d", i, d.StatusCode, j.StatusCode)
		}
		if d.ContentLength != j.ContentLength {
			t.Errorf("[%d] ContentLength: direct=%d fromJSONL=%d", i, d.ContentLength, j.ContentLength)
		}
		if d.Curl != j.Curl {
			t.Errorf("[%d] Curl: direct=%q fromJSONL=%q", i, d.Curl, j.Curl)
		}
	}
}

// TestCollectingWriter verifies that CollectingWriter stores results while delegating.
func TestCollectingWriter(t *testing.T) {
	var buf bytes.Buffer
	inner, _ := NewWriter("jsonl", &buf)
	cw := NewCollectingWriter(inner)

	results := []proute.ScanResult{
		makeScanResult("GET", "https://target.com/a", 200, 100),
		makeScanResult("POST", "https://target.com/b", 201, 50),
	}
	for _, r := range results {
		if err := cw.WriteResult(r); err != nil {
			t.Fatalf("WriteResult: %v", err)
		}
	}

	collected := cw.Results()
	if len(collected) != 2 {
		t.Errorf("collected %d results, want 2", len(collected))
	}
	if buf.Len() == 0 {
		t.Error("underlying writer received nothing")
	}
}
