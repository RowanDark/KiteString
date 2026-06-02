package cli

import (
	"fmt"
	"os"
	"time"

	ksout "github.com/RowanDark/kitestring/internal/output"
	"github.com/spf13/cobra"
)

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Generate markdown or HTML report from JSONL scan output",
	Long: `Generate a structured report from a KiteString JSONL results file.

Reports include an executive summary, per-finding reproduction steps, and
curl one-liners for every discovered endpoint.

Examples:
  ks report --input results.jsonl --format md --output report.md
  ks report --input results.jsonl --format html --output report.html
  ks report --input results.jsonl --format html   # writes to stdout`,
	RunE: func(cmd *cobra.Command, args []string) error {
		inputFile, _ := cmd.Flags().GetString("input")
		format, _ := cmd.Flags().GetString("format")
		outputFile, _ := cmd.Flags().GetString("output")

		if inputFile == "" {
			return fmt.Errorf("--input is required")
		}

		f, err := os.Open(inputFile)
		if err != nil {
			return fmt.Errorf("opening input: %w", err)
		}
		defer f.Close()

		report, err := ksout.FromJSONL(f)
		if err != nil {
			return fmt.Errorf("parsing JSONL: %w", err)
		}

		if report.Meta.KSVersion == "" {
			report.Meta.KSVersion = Version
		}
		if report.Meta.ScanDate.IsZero() {
			report.Meta.ScanDate = time.Now()
		}
		if report.Meta.Target == "" {
			report.Meta.Target = inputFile
		}

		var out *os.File
		if outputFile == "" || outputFile == "-" {
			out = os.Stdout
		} else {
			out, err = os.Create(outputFile)
			if err != nil {
				return fmt.Errorf("creating output file: %w", err)
			}
			defer out.Close()
		}

		if err := writeReport(report, format, out); err != nil {
			return err
		}

		if outputFile != "" && outputFile != "-" && !quiet {
			fmt.Fprintf(os.Stderr, "Report written to %s (%d finding(s))\n",
				outputFile, len(report.Findings))
		}
		return nil
	},
}

// writeReport dispatches to the correct report format writer.
func writeReport(r *ksout.Report, format string, w *os.File) error {
	switch format {
	case "md", "markdown":
		return r.WriteMarkdown(w)
	case "html":
		return r.WriteHTML(w)
	default:
		return fmt.Errorf("unsupported format %q: use md, markdown, or html", format)
	}
}

// writeAutoReport writes a report to a timestamped file in the current directory.
// format must be "md", "markdown", or "html". Returns the filename on success.
func writeAutoReport(r *ksout.Report, format string) (string, error) {
	ext := format
	if ext == "markdown" {
		ext = "md"
	}
	filename := fmt.Sprintf("ks-report-%s.%s", time.Now().UTC().Format("20060102-150405"), ext)
	f, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("creating report file: %w", err)
	}
	defer f.Close()
	if err := writeReport(r, format, f); err != nil {
		return "", err
	}
	return filename, nil
}

func init() {
	reportCmd.Flags().StringP("input", "i", "", "JSONL results file (required)")
	reportCmd.Flags().StringP("format", "f", "md", "output format: md, markdown, html")
	reportCmd.Flags().StringP("output", "O", "", "output file (default: stdout)")
}
