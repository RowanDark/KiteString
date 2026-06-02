package output

import (
	"fmt"
	"html"
	"io"
	"strings"
)

// WriteHTML writes the report as a self-contained single-file HTML document.
// No external dependencies; collapsible sections use <details> (pure CSS/HTML, no JS).
func (r *Report) WriteHTML(w io.Writer) error {
	ew := &errWriter{w: w}

	ew.write(htmlDocHeader)

	// Page header
	ew.write(`<div class="page-header">` + "\n")
	ew.write("  <h1>KiteString Scan Report</h1>\n")
	ew.write(`  <table class="meta-table">` + "\n")
	ew.writef("    <tr><td>Target</td><td>%s</td></tr>\n", html.EscapeString(r.Meta.Target))
	ew.writef("    <tr><td>Date</td><td>%s</td></tr>\n",
		r.Meta.ScanDate.UTC().Format("2006-01-02 15:04:05 UTC"))
	ew.writef("    <tr><td>KiteString</td><td>%s</td></tr>\n", html.EscapeString(r.Meta.KSVersion))
	if len(r.Meta.Wordlists) > 0 {
		ew.writef("    <tr><td>Wordlists</td><td>%s</td></tr>\n",
			html.EscapeString(strings.Join(r.Meta.Wordlists, ", ")))
	}
	if r.Meta.Duration > 0 {
		ew.writef("    <tr><td>Duration</td><td>%s</td></tr>\n", r.Meta.Duration)
	}
	ew.write("  </table>\n</div>\n\n")

	// Executive summary
	ew.write(`<div class="summary-section">` + "\n")
	ew.write("  <h2>Executive Summary</h2>\n")
	ew.writef("  <p>Total findings: <strong>%d</strong></p>\n", len(r.Findings))
	if len(r.ByStatus) > 0 {
		ew.write(`  <table class="summary-table">` + "\n")
		ew.write("    <thead><tr><th>Status Code</th><th>Count</th></tr></thead>\n    <tbody>\n")
		for _, sc := range r.ByStatus {
			cls := statusClass(sc.Code)
			ew.writef("      <tr><td class=\"status %s\">%d</td><td>%d</td></tr>\n",
				cls, sc.Code, sc.Count)
		}
		ew.write("    </tbody>\n  </table>\n")
	}
	ew.write("</div>\n\n")

	// Findings
	if len(r.Findings) > 0 {
		ew.write(`<div class="findings-section">` + "\n")
		ew.write("  <h2>Findings</h2>\n")
		for i, f := range r.Findings {
			path := extractPath(f.URL)
			cls := statusClass(f.StatusCode)
			ew.writef(`  <details class="finding">
    <summary class="finding-summary">
      <span class="finding-num">%d</span>
      <span class="method">%s</span>
      <span class="path">%s</span>
      <span class="status %s">%d</span>
    </summary>
    <div class="finding-body">
      <table class="meta-table">
        <tr><td>URL</td><td><code>%s</code></td></tr>
        <tr><td>Status</td><td class="status %s">%d</td></tr>
        <tr><td>Content Length</td><td>%d</td></tr>
        <tr><td>Response Time</td><td>%dms</td></tr>
        <tr><td>Timestamp</td><td>%s</td></tr>`,
				i+1,
				html.EscapeString(f.Method),
				html.EscapeString(path),
				cls, f.StatusCode,
				html.EscapeString(f.URL),
				cls, f.StatusCode,
				f.ContentLength,
				f.ResponseTimeMs,
				f.Timestamp.UTC().Format("2006-01-02 15:04:05 UTC"),
			)
			if f.Source != "" {
				ew.writef("\n        <tr><td>Source</td><td>%s</td></tr>", html.EscapeString(f.Source))
			}
			ew.writef(`
      </table>
      <div class="curl-block">
        <div class="block-label">Reproduction</div>
        <pre class="curl"><code>%s</code></pre>
      </div>
      <div class="notes-block">
        <div class="block-label">Notes</div>
        <p class="notes-placeholder">Add analysis notes here</p>
      </div>
    </div>
  </details>
`, html.EscapeString(f.Curl))
		}
		ew.write("</div>\n")
	}

	ew.write(htmlDocFooter)
	return ew.err
}

func statusClass(code int) string {
	return fmt.Sprintf("status-%dxx", code/100)
}

const htmlDocHeader = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>KiteString Scan Report</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{background:#0d1117;color:#c9d1d9;font-family:'Courier New',Courier,monospace;font-size:14px;line-height:1.6;padding:2rem;max-width:1100px;margin:0 auto}
h1{color:#58a6ff;font-size:1.8rem;margin-bottom:1rem}
h2{color:#79c0ff;font-size:1.2rem;margin:1.5rem 0 0.75rem}
strong{color:#e6edf3}
table{border-collapse:collapse;width:100%;margin-bottom:1rem}
td,th{padding:.4rem .8rem;border:1px solid #30363d;text-align:left;vertical-align:top}
th{background:#161b22;color:#8b949e;font-weight:normal}
tr:nth-child(even) td{background:#0d1117}
code{background:#161b22;border-radius:3px;padding:0 4px;font-family:inherit;color:#e6edf3;word-break:break-all}
pre.curl{background:#161b22;border:1px solid #30363d;border-radius:6px;padding:1rem;overflow-x:auto;white-space:pre-wrap;word-break:break-all;margin-top:.4rem}
pre.curl code{background:none;padding:0;color:#7ee787}
.page-header{border-bottom:1px solid #30363d;padding-bottom:1.25rem;margin-bottom:1.5rem}
.meta-table{width:auto}
.meta-table td:first-child{color:#8b949e;min-width:8rem;white-space:nowrap}
.summary-section{background:#161b22;border:1px solid #30363d;border-radius:6px;padding:1rem 1.5rem;margin-bottom:1.5rem}
.summary-table{margin-top:.75rem;width:auto}
.findings-section{margin-bottom:2rem}
details.finding{border:1px solid #30363d;border-radius:6px;margin-bottom:.6rem;overflow:hidden}
summary.finding-summary{display:flex;align-items:center;gap:.75rem;padding:.6rem 1rem;background:#161b22;cursor:pointer;user-select:none;list-style:none}
summary.finding-summary::-webkit-details-marker{display:none}
details[open] summary.finding-summary{border-bottom:1px solid #30363d}
.finding-num{color:#8b949e;min-width:1.8rem;font-size:.85rem}
.method{background:#0d419d;color:#79c0ff;padding:2px 8px;border-radius:4px;font-size:.8rem;font-weight:bold;min-width:3.5rem;text-align:center;white-space:nowrap}
.path{flex:1;color:#e6edf3;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.status{padding:2px 8px;border-radius:4px;font-weight:bold;font-size:.9rem;white-space:nowrap}
.status-2xx{background:#033a16;color:#3fb950}
.status-3xx{background:#341a00;color:#e3b341}
.status-4xx,.status-5xx{background:#3d1a1a;color:#f85149}
.finding-body{padding:1rem 1.5rem}
.finding-body .meta-table{margin-bottom:1rem}
.finding-body .meta-table td:first-child{color:#8b949e;width:9rem}
.curl-block,.notes-block{margin-top:1rem}
.block-label{font-size:.75rem;color:#8b949e;text-transform:uppercase;letter-spacing:.06em;margin-bottom:.3rem}
.notes-placeholder{color:#8b949e;font-style:italic}
@media print{
  body{background:#fff;color:#000;font-size:11pt;padding:1rem}
  h1,h2{color:#000}
  td,th{border-color:#ccc}
  th{background:#f0f0f0}
  details.finding{display:block;page-break-inside:avoid}
  summary.finding-summary{background:#f0f0f0;pointer-events:none}
  .method{background:#ddd;color:#000}
  .status-2xx{background:#e6f4ea;color:#006400}
  .status-3xx{background:#fefce8;color:#78350f}
  .status-4xx,.status-5xx{background:#fef2f2;color:#991b1b}
  pre.curl{background:#f8f8f8;border:1px solid #ddd}
  pre.curl code{color:#006400}
}
</style>
</head>
<body>
`

const htmlDocFooter = `</body>
</html>
`
