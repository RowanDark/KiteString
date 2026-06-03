package output

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/RowanDark/kitestring/pkg/proute"
)

// ReportMeta holds scan-level metadata for a report.
type ReportMeta struct {
	Target    string
	ScanDate  time.Time
	Wordlists []string
	Duration  time.Duration
	KSVersion string
}

// Finding represents a single interesting HTTP response with a user-editable notes field.
type Finding struct {
	Method         string
	Path           string
	URL            string
	StatusCode     int
	ContentLength  int
	ResponseTimeMs int64
	Timestamp      time.Time
	KSUID          string
	Notes          string
}

// Report aggregates scan metadata and findings for report generation.
type Report struct {
	Meta     ReportMeta
	Findings []Finding
}

// BuildReport constructs a Report from scan results and metadata.
func BuildReport(results []proute.ScanResult, meta ReportMeta) *Report {
	findings := make([]Finding, 0, len(results))
	for _, r := range results {
		findings = append(findings, Finding{
			Method:         r.Route.Method,
			Path:           r.Route.Path,
			URL:            r.URL,
			StatusCode:     r.StatusCode,
			ContentLength:  r.ContentLength,
			ResponseTimeMs: r.ResponseTime.Milliseconds(),
			Timestamp:      r.Timestamp,
			KSUID:          r.KSUID,
			Notes:          "Add analysis notes here",
		})
	}
	return &Report{Meta: meta, Findings: findings}
}

// jsonlEntry mirrors the JSONL output format from writer.go.
type jsonlEntry struct {
	Method         string `json:"method"`
	URL            string `json:"url"`
	StatusCode     int    `json:"status_code"`
	ContentLength  int    `json:"content_length"`
	ResponseTimeMs int64  `json:"response_time_ms"`
	Timestamp      string `json:"timestamp"`
	KSUID          string `json:"ksuid,omitempty"`
}

// FromJSONL builds a Report by reading a JSONL results file.
func FromJSONL(r io.Reader) (*Report, error) {
	var findings []Finding
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var entry jsonlEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("parsing JSONL line: %w", err)
		}
		ts, _ := time.Parse(time.RFC3339, entry.Timestamp)
		findings = append(findings, Finding{
			Method:         entry.Method,
			Path:           extractPath(entry.URL),
			URL:            entry.URL,
			StatusCode:     entry.StatusCode,
			ContentLength:  entry.ContentLength,
			ResponseTimeMs: entry.ResponseTimeMs,
			Timestamp:      ts,
			KSUID:          entry.KSUID,
			Notes:          "Add analysis notes here",
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading JSONL: %w", err)
	}

	meta := ReportMeta{ScanDate: time.Now()}
	if len(findings) > 0 {
		if u, err := url.Parse(findings[0].URL); err == nil {
			meta.Target = u.Host
		}
	}
	return &Report{Meta: meta, Findings: findings}, nil
}

// CurlCommand returns a curl one-liner for reproducing this finding.
func (f *Finding) CurlCommand() string {
	return fmt.Sprintf("curl -s -X %s '%s'", f.Method, f.URL)
}

func extractPath(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" {
		return "/"
	}
	return u.Path
}
