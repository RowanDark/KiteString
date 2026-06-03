package output

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// WriteMarkdown writes a Markdown-formatted report to w.
func (r *Report) WriteMarkdown(w io.Writer) error {
	// Header
	fmt.Fprintf(w, "# KiteString Scan Report\n\n")
	fmt.Fprintf(w, "| | |\n|---|---|\n")
	if r.Meta.Target != "" {
		fmt.Fprintf(w, "| **Target** | `%s` |\n", r.Meta.Target)
	}
	if !r.Meta.ScanDate.IsZero() {
		fmt.Fprintf(w, "| **Scan Date** | %s |\n", r.Meta.ScanDate.UTC().Format("2006-01-02 15:04:05 UTC"))
	}
	if r.Meta.KSVersion != "" {
		fmt.Fprintf(w, "| **KiteString Version** | %s |\n", r.Meta.KSVersion)
	}
	if len(r.Meta.Wordlists) > 0 {
		fmt.Fprintf(w, "| **Wordlists** | %s |\n", strings.Join(r.Meta.Wordlists, ", "))
	}
	if r.Meta.Duration > 0 {
		fmt.Fprintf(w, "| **Duration** | %s |\n", r.Meta.Duration.Round(time.Second))
	}
	fmt.Fprintf(w, "\n---\n\n")

	// Executive summary
	fmt.Fprintf(w, "## Executive Summary\n\n")
	fmt.Fprintf(w, "Total findings: **%d**\n\n", len(r.Findings))

	if len(r.Findings) > 0 {
		codeCounts := make(map[int]int)
		for _, f := range r.Findings {
			codeCounts[f.StatusCode]++
		}
		codes := make([]int, 0, len(codeCounts))
		for code := range codeCounts {
			codes = append(codes, code)
		}
		sort.Ints(codes)
		fmt.Fprintf(w, "| Status Code | Count |\n|---|---|\n")
		for _, code := range codes {
			fmt.Fprintf(w, "| %d | %d |\n", code, codeCounts[code])
		}
		fmt.Fprintf(w, "\n")
	}

	fmt.Fprintf(w, "---\n\n")

	// Findings
	fmt.Fprintf(w, "## Findings\n\n")
	if len(r.Findings) == 0 {
		fmt.Fprintf(w, "_No findings recorded._\n")
		return nil
	}

	for i, f := range r.Findings {
		fmt.Fprintf(w, "### %d. %s %s — HTTP %d\n\n", i+1, f.Method, f.Path, f.StatusCode)

		fmt.Fprintf(w, "| Field | Value |\n|---|---|\n")
		fmt.Fprintf(w, "| **URL** | `%s` |\n", f.URL)
		fmt.Fprintf(w, "| **Status** | %d |\n", f.StatusCode)
		fmt.Fprintf(w, "| **Content Length** | %d bytes |\n", f.ContentLength)
		fmt.Fprintf(w, "| **Response Time** | %dms |\n", f.ResponseTimeMs)
		if !f.Timestamp.IsZero() {
			fmt.Fprintf(w, "| **Timestamp** | %s |\n", f.Timestamp.UTC().Format("2006-01-02 15:04:05 UTC"))
		}
		if f.KSUID != "" {
			fmt.Fprintf(w, "| **KSUID** | `%s` |\n", f.KSUID)
		}
		fmt.Fprintf(w, "\n**Reproduction**\n\n")
		fmt.Fprintf(w, "```bash\n%s\n```\n\n", f.CurlCommand())
		fmt.Fprintf(w, "**Notes**\n\n> %s\n\n", f.Notes)

		if i < len(r.Findings)-1 {
			fmt.Fprintf(w, "---\n\n")
		}
	}
	return nil
}
