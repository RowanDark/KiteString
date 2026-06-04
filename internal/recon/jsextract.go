package recon

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/RowanDark/kitestring/pkg/proute"
)

var (
	// REST path patterns matched from string literals.
	reRESTAPIv = regexp.MustCompile(`["'` + "`" + `](/api/v\d+/[a-z][a-z\d/_-]*)["'` + "`" + `]`)
	reRESTv    = regexp.MustCompile(`["'` + "`" + `](/v\d+/[a-z][a-z\d/_-]*)["'` + "`" + `]`)
	reAPIBase  = regexp.MustCompile(`["'` + "`" + `]((?:/api|/rest|/service|/_api)/[a-zA-Z0-9/_-]+)["'` + "`" + `]`)
	reGraphQL  = regexp.MustCompile(`["'` + "`" + `](/(?:graphql|gql)(?:/[a-zA-Z0-9/_-]*)?)["'` + "`" + `]`)

	// fetch() — default GET.
	reFetch = regexp.MustCompile(`fetch\s*\(\s*["` + "`" + `']([^"` + "`" + `'\s]+)["` + "`" + `']`)

	// axios with method inference.
	reAxiosGet   = regexp.MustCompile(`axios\.get\s*\(\s*["` + "`" + `']([^"` + "`" + `'\s]+)["` + "`" + `']`)
	reAxiosPost  = regexp.MustCompile(`axios\.post\s*\(\s*["` + "`" + `']([^"` + "`" + `'\s]+)["` + "`" + `']`)
	reAxiosPut   = regexp.MustCompile(`axios\.put\s*\(\s*["` + "`" + `']([^"` + "`" + `'\s]+)["` + "`" + `']`)
	reAxiosDel   = regexp.MustCompile(`axios\.delete\s*\(\s*["` + "`" + `']([^"` + "`" + `'\s]+)["` + "`" + `']`)
	reAxiosPatch = regexp.MustCompile(`axios\.patch\s*\(\s*["` + "`" + `']([^"` + "`" + `'\s]+)["` + "`" + `']`)

	// Express-style route definitions.
	reExpressGet    = regexp.MustCompile(`(?:router|app)\.get\s*\(\s*["` + "`" + `']([^"` + "`" + `'\s]+)["` + "`" + `']`)
	reExpressPost   = regexp.MustCompile(`(?:router|app)\.post\s*\(\s*["` + "`" + `']([^"` + "`" + `'\s]+)["` + "`" + `']`)
	reExpressPut    = regexp.MustCompile(`(?:router|app)\.put\s*\(\s*["` + "`" + `']([^"` + "`" + `'\s]+)["` + "`" + `']`)
	reExpressDelete = regexp.MustCompile(`(?:router|app)\.delete\s*\(\s*["` + "`" + `']([^"` + "`" + `'\s]+)["` + "`" + `']`)
	reExpressPatch  = regexp.MustCompile(`(?:router|app)\.patch\s*\(\s*["` + "`" + `']([^"` + "`" + `'\s]+)["` + "`" + `']`)

	// HTML parsing patterns.
	reScriptSrc  = regexp.MustCompile(`(?i)<script[^>]+\bsrc\s*=\s*["']([^"']+)["']`)
	reAnchorHref = regexp.MustCompile(`(?i)<a[^>]+\bhref\s*=\s*["']([^"'#?][^"']*)["']`)

	// Static asset extensions to reject.
	staticExts = map[string]bool{
		".js": true, ".css": true, ".html": true, ".htm": true,
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
		".ico": true, ".svg": true, ".woff": true, ".woff2": true,
		".ttf": true, ".eot": true, ".map": true, ".txt": true,
		".xml": true, ".pdf": true, ".zip": true,
	}
)

// ExtractFromURL fetches a JS file at jsURL and extracts API routes.
// Each returned route has its Source field set to "js:<jsURL>".
func ExtractFromURL(jsURL string, client *http.Client) ([]proute.Route, error) {
	resp, err := client.Get(jsURL) //nolint:noctx // caller provides a pre-configured client; context threading is handled at the call site
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", jsURL, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", jsURL, err)
	}

	routes, err := ExtractFromBody(string(data))
	if err != nil {
		return nil, err
	}

	for i := range routes {
		routes[i].Source = "js:" + jsURL
	}
	return routes, nil
}

// ExtractFromBody parses raw JS content and returns deduplicated API routes.
// HTTP method is inferred where possible (axios.post → POST); defaults to GET.
func ExtractFromBody(body string) ([]proute.Route, error) {
	type candidate struct {
		path   string
		method string
	}

	var candidates []candidate

	add := func(method, path string) {
		path = stripQuery(path)
		if !isValidAPIPath(path) {
			return
		}
		candidates = append(candidates, candidate{path: path, method: method})
	}

	// REST path literals — method unknown, default GET.
	for _, m := range reRESTAPIv.FindAllStringSubmatch(body, -1) {
		add("GET", m[1])
	}
	for _, m := range reRESTv.FindAllStringSubmatch(body, -1) {
		add("GET", m[1])
	}
	for _, m := range reAPIBase.FindAllStringSubmatch(body, -1) {
		add("GET", m[1])
	}
	// GraphQL endpoints are almost always POSTed.
	for _, m := range reGraphQL.FindAllStringSubmatch(body, -1) {
		add("POST", m[1])
	}

	// fetch() — default GET.
	for _, m := range reFetch.FindAllStringSubmatch(body, -1) {
		add("GET", m[1])
	}

	// axios with explicit method.
	for _, m := range reAxiosGet.FindAllStringSubmatch(body, -1) {
		add("GET", m[1])
	}
	for _, m := range reAxiosPost.FindAllStringSubmatch(body, -1) {
		add("POST", m[1])
	}
	for _, m := range reAxiosPut.FindAllStringSubmatch(body, -1) {
		add("PUT", m[1])
	}
	for _, m := range reAxiosDel.FindAllStringSubmatch(body, -1) {
		add("DELETE", m[1])
	}
	for _, m := range reAxiosPatch.FindAllStringSubmatch(body, -1) {
		add("PATCH", m[1])
	}

	// Express-style route definitions.
	for _, m := range reExpressGet.FindAllStringSubmatch(body, -1) {
		add("GET", m[1])
	}
	for _, m := range reExpressPost.FindAllStringSubmatch(body, -1) {
		add("POST", m[1])
	}
	for _, m := range reExpressPut.FindAllStringSubmatch(body, -1) {
		add("PUT", m[1])
	}
	for _, m := range reExpressDelete.FindAllStringSubmatch(body, -1) {
		add("DELETE", m[1])
	}
	for _, m := range reExpressPatch.FindAllStringSubmatch(body, -1) {
		add("PATCH", m[1])
	}

	// Deduplicate by path. Prefer a non-GET method when there's a conflict.
	seen := make(map[string]string) // path → method
	for _, c := range candidates {
		existing, ok := seen[c.path]
		if !ok || existing == "GET" {
			seen[c.path] = c.method
		}
	}

	routes := make([]proute.Route, 0, len(seen))
	for path, method := range seen {
		routes = append(routes, proute.Route{
			Method: method,
			Path:   path,
		})
	}
	return routes, nil
}

// FindScriptURLs parses an HTML page body and returns absolute URLs for all
// <script src="..."> tags, resolved against baseURL.
func FindScriptURLs(pageBody, baseURL string) ([]string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL %q: %w", baseURL, err)
	}

	matches := reScriptSrc.FindAllStringSubmatch(pageBody, -1)
	seen := make(map[string]struct{}, len(matches))
	var out []string

	for _, m := range matches {
		parsed, parseErr := url.Parse(m[1])
		if parseErr != nil {
			continue
		}
		abs := base.ResolveReference(parsed).String()
		if _, dup := seen[abs]; dup {
			continue
		}
		seen[abs] = struct{}{}
		out = append(out, abs)
	}
	return out, nil
}

// CrawlAndExtract fetches the target root page, discovers <script> tags,
// extracts API routes from each JS file, and deduplicates the results.
// depth=1 processes the root page only; depth=2 also crawls pages linked
// from the root (same origin) one level deeper.
// inScope, if non-nil, is called with each script URL's hostname; URLs that
// return false are skipped without fetching.
func CrawlAndExtract(target proute.ScanTarget, client *http.Client, depth int, inScope func(string) bool) ([]proute.Route, error) {
	if depth < 1 {
		depth = 1
	}

	rootURL := targetRootURL(target)
	visited := make(map[string]bool)
	// Deduplicate extracted routes by path; prefer non-GET method on conflict.
	merged := make(map[string]proute.Route) // path → route

	var crawlPage func(pageURL string, remaining int) error
	crawlPage = func(pageURL string, remaining int) error {
		if visited[pageURL] {
			return nil
		}
		visited[pageURL] = true

		body, err := fetchBody(pageURL, client)
		if err != nil {
			return nil // skip unreachable pages silently
		}

		scriptURLs, err := FindScriptURLs(body, pageURL)
		if err != nil {
			return err
		}

		for _, jsURL := range scriptURLs {
			if inScope != nil {
				parsed, parseErr := url.Parse(jsURL)
				if parseErr != nil || !inScope(parsed.Hostname()) {
					continue
				}
			}
			routes, extractErr := ExtractFromURL(jsURL, client)
			if extractErr != nil {
				continue
			}
			for _, r := range routes {
				existing, ok := merged[r.Path]
				if !ok || existing.Method == "GET" {
					merged[r.Path] = r
				}
			}
		}

		if remaining <= 1 {
			return nil
		}

		// Find same-origin linked pages to crawl at next depth level.
		linkedPages := findLinkedPages(body, pageURL, target)
		for _, linked := range linkedPages {
			if err := crawlPage(linked, remaining-1); err != nil {
				return err
			}
		}
		return nil
	}

	if err := crawlPage(rootURL, depth); err != nil {
		return nil, err
	}

	routes := make([]proute.Route, 0, len(merged))
	for _, r := range merged {
		routes = append(routes, r)
	}
	return routes, nil
}

// findLinkedPages returns same-origin absolute URLs from <a href="..."> tags.
func findLinkedPages(pageBody, pageURL string, target proute.ScanTarget) []string {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}

	matches := reAnchorHref.FindAllStringSubmatch(pageBody, -1)
	seen := make(map[string]struct{})
	var out []string

	for _, m := range matches {
		parsed, parseErr := url.Parse(m[1])
		if parseErr != nil {
			continue
		}
		abs := base.ResolveReference(parsed)
		// Same-origin only.
		if abs.Hostname() != target.Host {
			continue
		}
		// Drop query strings and fragments for crawl dedup.
		abs.RawQuery = ""
		abs.Fragment = ""
		absStr := abs.String()
		if _, dup := seen[absStr]; dup {
			continue
		}
		seen[absStr] = struct{}{}
		out = append(out, absStr)
	}
	return out
}

// fetchBody performs a GET request and returns the response body as a string.
func fetchBody(rawURL string, client *http.Client) (string, error) {
	resp, err := client.Get(rawURL) //nolint:noctx // caller provides a pre-configured client; context threading is handled at the call site
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// targetRootURL constructs the root URL from a ScanTarget.
func targetRootURL(t proute.ScanTarget) string {
	scheme := t.Scheme
	if scheme == "" {
		scheme = "https"
	}
	host := t.Host
	isDefault := t.Port == 0 ||
		(scheme == "http" && t.Port == 80) ||
		(scheme == "https" && t.Port == 443)
	if !isDefault && t.Port > 0 {
		host = fmt.Sprintf("%s:%d", t.Host, t.Port)
	}
	base := t.BasePath
	if base == "" {
		base = "/"
	}
	return scheme + "://" + host + base
}

// isValidAPIPath reports whether path looks like an API endpoint worth scanning.
func isValidAPIPath(path string) bool {
	if len(path) < 2 || path[0] != '/' {
		return false
	}
	// Reject template literal expressions.
	if strings.Contains(path, "${") {
		return false
	}
	// Reject static asset extensions.
	lower := strings.ToLower(path)
	for ext := range staticExts {
		if strings.HasSuffix(lower, ext) {
			return false
		}
	}
	return true
}

// stripQuery removes the query string from a URL path, leaving only the path.
func stripQuery(raw string) string {
	if idx := strings.IndexByte(raw, '?'); idx >= 0 {
		return raw[:idx]
	}
	return raw
}
