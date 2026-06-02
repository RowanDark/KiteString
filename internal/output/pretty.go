package output

import (
	"fmt"
	"time"

	"github.com/RowanDark/kitestring/pkg/proute"
)

const (
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

type prettyWriter struct {
	out   interface{ Write([]byte) (int, error) }
	color bool
}

func (w *prettyWriter) WriteResult(r proute.ScanResult) error {
	curl := GenerateCurl(r)

	if w.color {
		clr := statusColor(r.StatusCode)
		_, err := fmt.Fprintf(w.out, "%s%-6s%s %s%d%s [%d] %s %s%s%s\n    %s%s%s\n",
			colorBold, r.Route.Method, colorReset,
			clr, r.StatusCode, colorReset,
			r.ContentLength,
			r.URL,
			colorDim, r.KSUID, colorReset,
			colorDim, curl, colorReset,
		)
		return err
	}

	_, err := fmt.Fprintf(w.out, "%-6s %d [%d] %s %s\n    %s\n",
		r.Route.Method,
		r.StatusCode,
		r.ContentLength,
		r.URL,
		r.KSUID,
		curl,
	)
	return err
}

func (w *prettyWriter) WriteError(err error, target string) error {
	if w.color {
		_, werr := fmt.Fprintf(w.out, "%s[ERR]%s %s: %v\n", colorRed, colorReset, target, err)
		return werr
	}
	_, werr := fmt.Fprintf(w.out, "[ERR] %s: %v\n", target, err)
	return werr
}

func (w *prettyWriter) WriteSummary(s ScanSummary) error {
	elapsed := s.Duration.Round(time.Millisecond)

	if w.color {
		_, err := fmt.Fprintf(w.out,
			"\n%s── Summary ──────────────────────────────%s\n"+
				"  Results:    %s%d%s\n"+
				"  Targets:    %d\n"+
				"  Routes:     %d\n"+
				"  Duration:   %s\n"+
				"  Quarantine: %d host(s)\n"+
				"%s─────────────────────────────────────────%s\n",
			colorBold, colorReset,
			colorGreen, s.TotalResults, colorReset,
			s.TotalTargets,
			s.TotalRoutes,
			elapsed,
			len(s.QuarantinedHosts),
			colorBold, colorReset,
		)
		return err
	}

	_, err := fmt.Fprintf(w.out,
		"\n── Summary ──────────────────────────────\n"+
			"  Results:    %d\n"+
			"  Targets:    %d\n"+
			"  Routes:     %d\n"+
			"  Duration:   %s\n"+
			"  Quarantine: %d host(s)\n"+
			"─────────────────────────────────────────\n",
		s.TotalResults, s.TotalTargets, s.TotalRoutes, elapsed, len(s.QuarantinedHosts),
	)
	return err
}

func (w *prettyWriter) Flush() error { return nil }
