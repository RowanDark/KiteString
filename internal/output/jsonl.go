package output

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/RowanDark/kitestring/pkg/proute"
)

type jsonlWriter struct {
	out interface{ Write([]byte) (int, error) }
}

type jsonlResult struct {
	Method         string `json:"method"`
	URL            string `json:"url"`
	Status         int    `json:"status"`
	ContentLength  int    `json:"content_length"`
	ResponseTimeMs int64  `json:"response_time_ms"`
	Timestamp      string `json:"timestamp"`
	KSUID          string `json:"ksuid,omitempty"`
	Source         string `json:"source,omitempty"`
	Curl           string `json:"curl"`
}

type jsonlError struct {
	Type   string `json:"type"`
	Target string `json:"target"`
	Error  string `json:"error"`
}

type jsonlSummary struct {
	Type             string   `json:"type"`
	TotalTargets     int      `json:"total_targets"`
	TotalRoutes      int      `json:"total_routes"`
	TotalResults     int      `json:"total_results"`
	DurationMs       int64    `json:"duration_ms"`
	QuarantinedHosts []string `json:"quarantined_hosts"`
}

func (w *jsonlWriter) WriteResult(r proute.ScanResult) error {
	jr := jsonlResult{
		Method:         r.Route.Method,
		URL:            r.URL,
		Status:         r.StatusCode,
		ContentLength:  r.ContentLength,
		ResponseTimeMs: r.ResponseTime.Milliseconds(),
		Timestamp:      r.Timestamp.UTC().Format(time.RFC3339),
		KSUID:          r.KSUID,
		Source:         r.Route.Source,
		Curl:           GenerateCurl(r),
	}
	b, err := json.Marshal(jr)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w.out, "%s\n", b)
	return err
}

func (w *jsonlWriter) WriteError(err error, target string) error {
	je := jsonlError{
		Type:   "error",
		Target: target,
		Error:  err.Error(),
	}
	b, merr := json.Marshal(je)
	if merr != nil {
		return merr
	}
	_, werr := fmt.Fprintf(w.out, "%s\n", b)
	return werr
}

func (w *jsonlWriter) WriteSummary(s ScanSummary) error {
	hosts := s.QuarantinedHosts
	if hosts == nil {
		hosts = []string{}
	}
	js := jsonlSummary{
		Type:             "summary",
		TotalTargets:     s.TotalTargets,
		TotalRoutes:      s.TotalRoutes,
		TotalResults:     s.TotalResults,
		DurationMs:       s.Duration.Milliseconds(),
		QuarantinedHosts: hosts,
	}
	b, err := json.Marshal(js)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w.out, "%s\n", b)
	return err
}

func (w *jsonlWriter) Flush() error { return nil }
