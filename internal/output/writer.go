package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/RowanDark/kitestring/pkg/proute"
)

const (
	colorGreen  = "\033[32m"
	colorCyan   = "\033[36m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorReset  = "\033[0m"
)

// Writer formats and emits scan results.
type Writer struct {
	out    io.Writer
	format string
}

// New returns a Writer using the given format. Pass nil for out to use os.Stdout.
func New(format string, out io.Writer) *Writer {
	if out == nil {
		out = os.Stdout
	}
	if format == "" {
		format = "pretty"
	}
	return &Writer{out: out, format: format}
}

// SetWriter redirects output to w (useful for testing).
func (w *Writer) SetWriter(out io.Writer) {
	w.out = out
}

// Write formats and emits a single ScanResult.
func (w *Writer) Write(r proute.ScanResult) {
	switch w.format {
	case "jsonl":
		w.writeJSONL(r)
	case "text":
		w.writeText(r)
	default:
		w.writePretty(r)
	}
}

type jsonResult struct {
	Method         string `json:"method"`
	URL            string `json:"url"`
	StatusCode     int    `json:"status_code"`
	ContentLength  int    `json:"content_length"`
	ResponseTimeMs int64  `json:"response_time_ms"`
	Timestamp      string `json:"timestamp"`
	KSUID          string `json:"ksuid,omitempty"`
}

func (w *Writer) writeJSONL(r proute.ScanResult) {
	jr := jsonResult{
		Method:         r.Route.Method,
		URL:            r.URL,
		StatusCode:     r.StatusCode,
		ContentLength:  r.ContentLength,
		ResponseTimeMs: r.ResponseTime.Milliseconds(),
		Timestamp:      r.Timestamp.Format(time.RFC3339),
		KSUID:          r.KSUID,
	}
	b, _ := json.Marshal(jr)
	fmt.Fprintf(w.out, "%s\n", b)
}

func (w *Writer) writeText(r proute.ScanResult) {
	fmt.Fprintf(w.out, "%-6s  %d  %-8d  %-10s  %s\n",
		r.Route.Method,
		r.StatusCode,
		r.ContentLength,
		r.ResponseTime.Round(time.Millisecond),
		r.URL,
	)
}

func (w *Writer) writePretty(r proute.ScanResult) {
	color := statusColor(r.StatusCode)
	fmt.Fprintf(w.out, "%s%d%s  %-6s  %-8d  %-10s  %s\n",
		color, r.StatusCode, colorReset,
		r.Route.Method,
		r.ContentLength,
		r.ResponseTime.Round(time.Millisecond),
		r.URL,
	)
}

func statusColor(code int) string {
	switch {
	case code >= 200 && code < 300:
		return colorGreen
	case code >= 300 && code < 400:
		return colorCyan
	case code >= 400 && code < 500:
		return colorYellow
	default:
		return colorRed
	}
}
