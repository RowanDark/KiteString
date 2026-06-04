package cli

import (
	"fmt"
	"os"

	ksoutput "github.com/RowanDark/kitestring/internal/output"
	"github.com/spf13/cobra"
)

var reportCmd = &cobra.Command{
	Use:     "report",
	Aliases: []string{"rp"},
	Short:   "Generate Markdown or HTML report from scan results",
	Long: `Generate a structured report from a JSONL results file.

Reports are formatted for bug bounty submission workflows: each finding
includes full reproduction steps, response metadata, and a curl one-liner.

Examples:
  ks report --input results.jsonl --format html --output report.html
  ks report --input results.jsonl --format md --output report.md
  ks report --input results.jsonl --format markdown`,
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

		report, err := ksoutput.FromJSONL(f)
		if err != nil {
			return fmt.Errorf("parsing results: %w", err)
		}
		report.Meta.KSVersion = Version

		if !quiet {
			fmt.Fprintf(os.Stderr, "Generating %s report from %d findings...\n",
				format, len(report.Findings))
		}

		var out *os.File
		if outputFile == "" {
			out = os.Stdout
		} else {
			out, err = os.Create(outputFile)
			if err != nil {
				return fmt.Errorf("creating output file: %w", err)
			}
			defer out.Close()
		}

		switch format {
		case "html":
			err = report.WriteHTML(out)
		case "md", "markdown":
			err = report.WriteMarkdown(out)
		default:
			return fmt.Errorf("unknown format %q: use md, markdown, or html", format)
		}
		if err != nil {
			return fmt.Errorf("writing report: %w", err)
		}

		if outputFile != "" && !quiet {
			fmt.Fprintf(os.Stderr, "Report written to %s\n", outputFile)
		}
		return nil
	},
}

func init() {
	reportCmd.Flags().StringP("input", "i", "", "JSONL results file to generate report from (required)")
	reportCmd.Flags().StringP("format", "f", "md", "report format: md, markdown, html")
	reportCmd.Flags().StringP("output", "O", "", "output file path (default: stdout)")
}
