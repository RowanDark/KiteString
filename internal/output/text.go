package output

import (
	"fmt"
	"time"

	"github.com/RowanDark/kitestring/pkg/proute"
)

type textWriter struct {
	out interface{ Write([]byte) (int, error) }
}

func (w *textWriter) WriteResult(r proute.ScanResult) error {
	curl := GenerateCurl(r)
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

func (w *textWriter) WriteError(err error, target string) error {
	_, werr := fmt.Fprintf(w.out, "[ERR] %s: %v\n", target, err)
	return werr
}

func (w *textWriter) WriteSummary(s ScanSummary) error {
	_, err := fmt.Fprintf(w.out, "# SUMMARY: %d results, %d targets, %d routes, %s elapsed, %d quarantined\n",
		s.TotalResults,
		s.TotalTargets,
		s.TotalRoutes,
		s.Duration.Round(time.Millisecond),
		len(s.QuarantinedHosts),
	)
	return err
}

func (w *textWriter) Flush() error { return nil }
