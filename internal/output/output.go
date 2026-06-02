package output

import (
	"io"
	"os"
	"time"

	"github.com/RowanDark/kitestring/pkg/proute"
	"golang.org/x/term"
)

// Writer formats and emits scan results to an output destination.
type Writer interface {
	WriteResult(result proute.ScanResult) error
	WriteError(err error, target string) error
	WriteSummary(summary ScanSummary) error
	Flush() error
}

// ScanSummary holds aggregate statistics for a completed scan.
type ScanSummary struct {
	TotalTargets     int
	TotalRoutes      int
	TotalResults     int
	Duration         time.Duration
	QuarantinedHosts []string
}

// NewWriter returns the correct Writer implementation for the given format.
// Pass nil for out to write to os.Stdout. Unknown formats default to pretty.
func NewWriter(format string, out io.Writer) (Writer, error) {
	if out == nil {
		out = os.Stdout
	}
	switch format {
	case "jsonl":
		return &jsonlWriter{out: out}, nil
	case "text":
		return &textWriter{out: out}, nil
	default:
		return &prettyWriter{out: out, color: isTTY(out)}, nil
	}
}

func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func statusColor(code int) string {
	switch {
	case code >= 200 && code < 300:
		return colorGreen
	case code >= 300 && code < 400:
		return colorYellow
	case code >= 400 && code < 600:
		return colorRed
	default:
		return colorReset
	}
}
