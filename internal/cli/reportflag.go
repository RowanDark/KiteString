package cli

import (
	"fmt"
	"os"
	"time"

	ksoutput "github.com/RowanDark/kitestring/internal/output"
	"github.com/RowanDark/kitestring/pkg/proute"
)

// writeAutoReport generates a report file from results when --report is set.
// format is "md", "markdown", or "html". Returns the path written, or an error.
func writeAutoReport(results []proute.ScanResult, meta ksoutput.ReportMeta, format string) (string, error) {
	ext := "md"
	if format == "html" {
		ext = "html"
	}
	filename := fmt.Sprintf("ks-report-%s.%s", time.Now().Format("20060102-150405"), ext)

	f, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("creating report file: %w", err)
	}
	defer f.Close()

	report := ksoutput.BuildReport(results, meta)

	switch format {
	case "html":
		err = report.WriteHTML(f)
	default:
		err = report.WriteMarkdown(f)
	}
	if err != nil {
		return "", fmt.Errorf("writing report: %w", err)
	}
	return filename, nil
}
