package output

import (
	"fmt"
	"io"
	"net/url"
	"strings"
)

// WriteMarkdown writes the report as a GitHub-flavored Markdown document.
func (r *Report) WriteMarkdown(w io.Writer) error {
	ew := &errWriter{w: w}

	ew.writef("# KiteString Scan Report\n\n")
	ew.writef("| | |\n|---|---|\n")
	ew.writef("| **Target** | %s |\n", r.Meta.Target)
	ew.writef("| **Date** | %s |\n", r.Meta.ScanDate.UTC().Format("2006-01-02 15:04:05 UTC"))
	ew.writef("| **KiteString** | %s |\n", r.Meta.KSVersion)
	if len(r.Meta.Wordlists) > 0 {
		ew.writef("| **Wordlists** | %s |\n", strings.Join(r.Meta.Wordlists, ", "))
	}
	if r.Meta.Duration > 0 {
		ew.writef("| **Duration** | %s |\n", r.Meta.Duration)
	}
	ew.write("\n")

	ew.write("## Executive Summary\n\n")
	ew.writef("Total findings: **%d**\n\n", len(r.Findings))
	if len(r.ByStatus) > 0 {
		ew.write("| Status Code | Count |\n|---|---|\n")
		for _, sc := range r.ByStatus {
			ew.writef("| %d | %d |\n", sc.Code, sc.Count)
		}
		ew.write("\n")
	}

	if len(r.Findings) > 0 {
		ew.write("## Findings\n\n")
	}
	for i, f := range r.Findings {
		path := extractPath(f.URL)
		ew.writef("### %d. %s %s — %d\n\n", i+1, f.Method, path, f.StatusCode)

		ew.write("| Field | Value |\n|---|---|\n")
		ew.writef("| **URL** | `%s` |\n", f.URL)
		ew.writef("| **Status** | %d |\n", f.StatusCode)
		ew.writef("| **Content Length** | %d |\n", f.ContentLength)
		ew.writef("| **Response Time** | %dms |\n", f.ResponseTimeMs)
		ew.writef("| **Timestamp** | %s |\n", f.Timestamp.UTC().Format("2006-01-02 15:04:05 UTC"))
		if f.Source != "" {
			ew.writef("| **Source** | %s |\n", f.Source)
		}
		ew.write("\n")

		ew.writef("**Reproduction:**\n\n```bash\n%s\n```\n\n", f.Curl)
		ew.writef("**Notes:**\n\n%s\n\n", f.Notes)
		ew.write("---\n\n")
	}

	return ew.err
}

func extractPath(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" {
		return rawURL
	}
	if u.RawQuery != "" {
		return u.Path + "?" + u.RawQuery
	}
	return u.Path
}

// errWriter accumulates writes and short-circuits on the first error.
type errWriter struct {
	w   io.Writer
	err error
}

func (ew *errWriter) write(s string) {
	if ew.err != nil {
		return
	}
	_, ew.err = io.WriteString(ew.w, s)
}

func (ew *errWriter) writef(format string, args ...interface{}) {
	if ew.err != nil {
		return
	}
	_, ew.err = fmt.Fprintf(ew.w, format, args...)
}
