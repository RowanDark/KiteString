package output

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const htmlHeader = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>KiteString Scan Report</title>
<style>
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }

:root {
  --bg: #0d1117;
  --surface: #161b22;
  --border: #30363d;
  --text: #c9d1d9;
  --muted: #8b949e;
  --accent: #58a6ff;
  --green: #3fb950;
  --yellow: #d29922;
  --orange: #f0883e;
  --red: #f85149;
  --code-bg: #1f2428;
  --tag-bg: #21262d;
}

body {
  background: var(--bg);
  color: var(--text);
  font-family: 'Segoe UI', system-ui, sans-serif;
  font-size: 14px;
  line-height: 1.6;
  padding: 2rem 1rem;
}

.container { max-width: 960px; margin: 0 auto; }

h1 { font-size: 1.8rem; color: var(--accent); margin-bottom: 1.5rem; }
h2 { font-size: 1.2rem; color: var(--text); margin: 2rem 0 1rem; border-bottom: 1px solid var(--border); padding-bottom: 0.5rem; }

.meta-table, .finding-meta { width: 100%; border-collapse: collapse; margin-bottom: 1.5rem; }
.meta-table td, .finding-meta td { padding: 0.4rem 0.75rem; border: 1px solid var(--border); vertical-align: top; }
.meta-table td:first-child, .finding-meta td:first-child { color: var(--muted); white-space: nowrap; width: 160px; font-weight: 600; }

.summary-table { width: 100%; border-collapse: collapse; margin-bottom: 1.5rem; }
.summary-table th, .summary-table td { padding: 0.5rem 0.75rem; border: 1px solid var(--border); text-align: left; }
.summary-table th { background: var(--surface); color: var(--muted); font-weight: 600; }

code, .mono { font-family: 'Cascadia Code', 'Fira Code', 'Consolas', monospace; font-size: 0.85em; }

.code-block {
  background: var(--code-bg);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 0.75rem 1rem;
  overflow-x: auto;
  margin: 0.5rem 0 1rem;
}
.code-block code { color: #79c0ff; }
.curl-cmd .kw  { color: #ff7b72; }
.curl-cmd .flag { color: #d2a8ff; }
.curl-cmd .url { color: #a5d6ff; }

details { background: var(--surface); border: 1px solid var(--border); border-radius: 8px; margin-bottom: 1rem; }
summary {
  padding: 0.75rem 1rem;
  cursor: pointer;
  list-style: none;
  display: flex;
  align-items: center;
  gap: 0.75rem;
  user-select: none;
}
summary::-webkit-details-marker { display: none; }
summary::before { content: '▶'; font-size: 0.7em; color: var(--muted); transition: transform 0.15s; }
details[open] summary::before { transform: rotate(90deg); }

.finding-body { padding: 0 1rem 1rem; }

.badge {
  display: inline-block;
  padding: 0.15rem 0.5rem;
  border-radius: 4px;
  font-family: monospace;
  font-size: 0.8em;
  font-weight: 700;
}
.badge-2xx { background: #1a3d2b; color: var(--green); }
.badge-3xx { background: #1a2d3d; color: var(--accent); }
.badge-4xx { background: #3d2b1a; color: var(--orange); }
.badge-5xx { background: #3d1a1a; color: var(--red); }
.badge-other { background: var(--tag-bg); color: var(--muted); }

.method { font-family: monospace; font-weight: 700; color: var(--yellow); }
.path   { font-family: monospace; color: var(--text); }

.notes-block {
  background: #161b22;
  border-left: 3px solid var(--border);
  padding: 0.5rem 1rem;
  color: var(--muted);
  font-style: italic;
  margin-top: 0.5rem;
}

@media print {
  body { background: #fff; color: #111; font-size: 11pt; }
  :root {
    --bg: #fff; --surface: #f5f5f5; --border: #ccc; --text: #111; --muted: #555;
    --accent: #0366d6; --code-bg: #f5f5f5;
  }
  details { page-break-inside: avoid; }
  details[open] { border-color: #ccc; }
  summary { cursor: default; }
  .code-block code { color: #005cc5; }
}
</style>
</head>
<body>
<div class="container">
`

const htmlFooter = `</div>
</body>
</html>
`

// WriteHTML writes a self-contained single-file HTML report to w.
func (r *Report) WriteHTML(w io.Writer) error {
	fmt.Fprint(w, htmlHeader)

	fmt.Fprint(w, "<h1>KiteString Scan Report</h1>\n")

	// Metadata table
	fmt.Fprint(w, "<table class=\"meta-table\">\n")
	if r.Meta.Target != "" {
		fmt.Fprintf(w, "<tr><td>Target</td><td><code>%s</code></td></tr>\n", html.EscapeString(r.Meta.Target))
	}
	if !r.Meta.ScanDate.IsZero() {
		fmt.Fprintf(w, "<tr><td>Scan Date</td><td>%s</td></tr>\n", r.Meta.ScanDate.UTC().Format("2006-01-02 15:04:05 UTC"))
	}
	if r.Meta.KSVersion != "" {
		fmt.Fprintf(w, "<tr><td>KiteString Version</td><td>%s</td></tr>\n", html.EscapeString(r.Meta.KSVersion))
	}
	if len(r.Meta.Wordlists) > 0 {
		fmt.Fprintf(w, "<tr><td>Wordlists</td><td>%s</td></tr>\n", html.EscapeString(strings.Join(r.Meta.Wordlists, ", ")))
	}
	if r.Meta.Duration > 0 {
		fmt.Fprintf(w, "<tr><td>Duration</td><td>%s</td></tr>\n", r.Meta.Duration.Round(time.Second))
	}
	fmt.Fprint(w, "</table>\n")

	// Executive summary
	fmt.Fprint(w, "<h2>Executive Summary</h2>\n")
	fmt.Fprintf(w, "<p>Total findings: <strong>%d</strong></p>\n", len(r.Findings))

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

		fmt.Fprint(w, "<table class=\"summary-table\">\n<thead><tr><th>Status Code</th><th>Count</th></tr></thead>\n<tbody>\n")
		for _, code := range codes {
			fmt.Fprintf(w, "<tr><td>%s</td><td>%d</td></tr>\n",
				statusBadgeHTML(code), codeCounts[code])
		}
		fmt.Fprint(w, "</tbody></table>\n")
	}

	// Findings
	fmt.Fprint(w, "<h2>Findings</h2>\n")
	if len(r.Findings) == 0 {
		fmt.Fprint(w, "<p><em>No findings recorded.</em></p>\n")
		fmt.Fprint(w, htmlFooter)
		return nil
	}

	for i, f := range r.Findings {
		fmt.Fprintf(w, "<details open>\n<summary>")
		fmt.Fprintf(w, "<span>%d.</span> %s <span class=\"method\">%s</span> <span class=\"path\">%s</span> %s",
			i+1,
			statusBadgeHTML(f.StatusCode),
			html.EscapeString(f.Method),
			html.EscapeString(f.Path),
			"",
		)
		fmt.Fprintf(w, "</summary>\n<div class=\"finding-body\">\n")

		// Metadata table
		fmt.Fprint(w, "<table class=\"finding-meta\">\n")
		fmt.Fprintf(w, "<tr><td>URL</td><td><code>%s</code></td></tr>\n", html.EscapeString(f.URL))
		fmt.Fprintf(w, "<tr><td>Status</td><td>%s</td></tr>\n", statusBadgeHTML(f.StatusCode))
		fmt.Fprintf(w, "<tr><td>Content Length</td><td>%d bytes</td></tr>\n", f.ContentLength)
		fmt.Fprintf(w, "<tr><td>Response Time</td><td>%dms</td></tr>\n", f.ResponseTimeMs)
		if !f.Timestamp.IsZero() {
			fmt.Fprintf(w, "<tr><td>Timestamp</td><td>%s</td></tr>\n", f.Timestamp.UTC().Format("2006-01-02 15:04:05 UTC"))
		}
		if f.KSUID != "" {
			fmt.Fprintf(w, "<tr><td>KSUID</td><td><code>%s</code></td></tr>\n", html.EscapeString(f.KSUID))
		}
		fmt.Fprint(w, "</table>\n")

		// Curl reproduction block
		fmt.Fprint(w, "<p><strong>Reproduction</strong></p>\n")
		fmt.Fprintf(w, "<div class=\"code-block\"><code class=\"curl-cmd\">%s</code></div>\n",
			formatCurlHTML(f.CurlCommand()))

		// Notes
		fmt.Fprint(w, "<p><strong>Notes</strong></p>\n")
		fmt.Fprintf(w, "<div class=\"notes-block\">%s</div>\n", html.EscapeString(f.Notes))

		fmt.Fprint(w, "</div>\n</details>\n")
	}

	fmt.Fprint(w, htmlFooter)
	return nil
}

// statusBadgeHTML returns an HTML badge span for an HTTP status code.
func statusBadgeHTML(code int) string {
	class := "badge-other"
	switch {
	case code >= 200 && code < 300:
		class = "badge-2xx"
	case code >= 300 && code < 400:
		class = "badge-3xx"
	case code >= 400 && code < 500:
		class = "badge-4xx"
	case code >= 500:
		class = "badge-5xx"
	}
	return fmt.Sprintf(`<span class="badge %s">%s %d</span>`,
		class, html.EscapeString(http.StatusText(code)), code)
}

// formatCurlHTML syntax-highlights a curl command with spans.
func formatCurlHTML(cmd string) string {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return html.EscapeString(cmd)
	}
	var sb strings.Builder
	for i, p := range parts {
		if i > 0 {
			sb.WriteByte(' ')
		}
		switch {
		case p == "curl":
			fmt.Fprintf(&sb, `<span class="kw">%s</span>`, html.EscapeString(p))
		case strings.HasPrefix(p, "-"):
			fmt.Fprintf(&sb, `<span class="flag">%s</span>`, html.EscapeString(p))
		default:
			fmt.Fprintf(&sb, `<span class="url">%s</span>`, html.EscapeString(p))
		}
	}
	return sb.String()
}
