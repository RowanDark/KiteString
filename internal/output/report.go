package output

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/RowanDark/kitestring/pkg/proute"
)

// ReportMeta holds scan-level metadata attached to a report.
type ReportMeta struct {
	Target    string
	ScanDate  time.Time
	Wordlists []string
	Duration  time.Duration
	KSVersion string
}

// Finding is a single scanned result enriched with report-specific fields.
type Finding struct {
	Method         string
	URL            string
	StatusCode     int
	ContentLength  int
	ResponseTimeMs int64
	Timestamp      time.Time
	KSUID          string
	Source         string
	Curl           string
	Notes          string // user-editable placeholder
}

// StatusCount holds the number of findings for one HTTP status code.
type StatusCount struct {
	Code  int
	Count int
}

// Report is the top-level data model for both markdown and HTML outputs.
type Report struct {
	Meta     ReportMeta
	Findings []Finding
	ByStatus []StatusCount // sorted by count desc, then code asc
}

// BuildReport constructs a Report from a slice of scan results and metadata.
func BuildReport(results []proute.ScanResult, meta ReportMeta) *Report {
	r := &Report{Meta: meta}
	counts := make(map[int]int)
	for _, sr := range results {
		f := Finding{
			Method:         sr.Route.Method,
			URL:            sr.URL,
			StatusCode:     sr.StatusCode,
			ContentLength:  sr.ContentLength,
			ResponseTimeMs: sr.ResponseTime.Milliseconds(),
			Timestamp:      sr.Timestamp,
			KSUID:          sr.KSUID,
			Source:         sr.Route.Source,
			Curl:           GenerateCurl(sr),
			Notes:          "> Add analysis notes here",
		}
		r.Findings = append(r.Findings, f)
		counts[sr.StatusCode]++
	}
	r.ByStatus = buildStatusSummary(counts)
	return r
}

// FromJSONL builds a Report by reading a JSONL results file produced by KiteString.
// Lines with type "summary" populate Meta.Duration; result lines become Findings.
func FromJSONL(rd io.Reader) (*Report, error) {
	r := &Report{}
	counts := make(map[int]int)

	sc := bufio.NewScanner(rd)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}

		var envelope struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(line, &envelope)

		switch envelope.Type {
		case "error":
			continue
		case "summary":
			var s jsonlSummary
			if err := json.Unmarshal(line, &s); err == nil {
				r.Meta.Duration = time.Duration(s.DurationMs) * time.Millisecond
			}
		default:
			var jr jsonlResult
			if err := json.Unmarshal(line, &jr); err != nil {
				return nil, fmt.Errorf("parsing JSONL line: %w", err)
			}
			ts, _ := time.Parse(time.RFC3339, jr.Timestamp)
			f := Finding{
				Method:         jr.Method,
				URL:            jr.URL,
				StatusCode:     jr.Status,
				ContentLength:  jr.ContentLength,
				ResponseTimeMs: jr.ResponseTimeMs,
				Timestamp:      ts,
				KSUID:          jr.KSUID,
				Source:         jr.Source,
				Curl:           jr.Curl,
				Notes:          "> Add analysis notes here",
			}
			r.Findings = append(r.Findings, f)
			counts[jr.Status]++
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	r.ByStatus = buildStatusSummary(counts)
	return r, nil
}

func buildStatusSummary(counts map[int]int) []StatusCount {
	out := make([]StatusCount, 0, len(counts))
	for code, n := range counts {
		out = append(out, StatusCount{Code: code, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// CollectingWriter wraps a Writer and retains every ScanResult written through it.
type CollectingWriter struct {
	inner   Writer
	mu      sync.Mutex
	results []proute.ScanResult
}

// NewCollectingWriter returns a CollectingWriter that delegates all calls to inner.
func NewCollectingWriter(inner Writer) *CollectingWriter {
	return &CollectingWriter{inner: inner}
}

func (c *CollectingWriter) WriteResult(r proute.ScanResult) error {
	c.mu.Lock()
	c.results = append(c.results, r)
	c.mu.Unlock()
	return c.inner.WriteResult(r)
}

func (c *CollectingWriter) WriteError(err error, target string) error {
	return c.inner.WriteError(err, target)
}

func (c *CollectingWriter) WriteSummary(s ScanSummary) error {
	return c.inner.WriteSummary(s)
}

func (c *CollectingWriter) Flush() error {
	return c.inner.Flush()
}

// Results returns a snapshot of the collected scan results.
func (c *CollectingWriter) Results() []proute.ScanResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]proute.ScanResult, len(c.results))
	copy(out, c.results)
	return out
}
