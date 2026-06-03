package recon_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/RowanDark/kitestring/internal/recon"
)

// fixtureJS is a representative JS snippet containing various API call patterns.
const fixtureJS = `
// REST fetch calls
fetch('/api/v1/users')
fetch("/api/v1/products")

// axios with method inference
axios.get('/v2/items')
axios.post('/api/v1/login')
axios.put('/api/v1/users/profile')
axios.delete('/api/v1/sessions')
axios.patch('/api/v1/settings')

// Express-style routes
router.get('/health', handler)
app.post('/api/v1/register', handler)

// GraphQL
fetch('/graphql')

// Duplicates — should be deduped
fetch('/api/v1/users')
axios.get('/api/v1/users')
`

func TestExtractFromBody_ReturnsExpectedRoutes(t *testing.T) {
	routes, err := recon.ExtractFromBody(fixtureJS)
	if err != nil {
		t.Fatalf("ExtractFromBody error: %v", err)
	}

	// Build a map for easy lookup.
	byPath := make(map[string]string, len(routes))
	for _, r := range routes {
		byPath[r.Path] = r.Method
	}

	tests := []struct {
		path   string
		method string
	}{
		{"/api/v1/users", "GET"},
		{"/api/v1/products", "GET"},
		{"/v2/items", "GET"},
		{"/api/v1/login", "POST"},
		{"/api/v1/users/profile", "PUT"},
		{"/api/v1/sessions", "DELETE"},
		{"/api/v1/settings", "PATCH"},
		{"/health", "GET"},
		{"/api/v1/register", "POST"},
		{"/graphql", "POST"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got, ok := byPath[tc.path]
			if !ok {
				t.Errorf("path %q not found in extracted routes", tc.path)
				return
			}
			if got != tc.method {
				t.Errorf("path %q: got method %q, want %q", tc.path, got, tc.method)
			}
		})
	}
}

func TestExtractFromBody_Deduplication(t *testing.T) {
	js := `
fetch('/api/v1/users')
fetch('/api/v1/users')
axios.get('/api/v1/users')
axios.post('/api/v1/users')
`
	routes, err := recon.ExtractFromBody(js)
	if err != nil {
		t.Fatalf("ExtractFromBody error: %v", err)
	}

	count := 0
	for _, r := range routes {
		if r.Path == "/api/v1/users" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected /api/v1/users to appear exactly once, got %d", count)
	}
}

func TestExtractFromBody_MethodInference(t *testing.T) {
	js := `axios.post('/api/v1/login', {username, password})`

	routes, err := recon.ExtractFromBody(js)
	if err != nil {
		t.Fatalf("ExtractFromBody error: %v", err)
	}

	for _, r := range routes {
		if r.Path == "/api/v1/login" {
			if r.Method != "POST" {
				t.Errorf("expected POST for /api/v1/login, got %q", r.Method)
			}
			return
		}
	}
	t.Error("/api/v1/login not found in extracted routes")
}

func TestExtractFromBody_IgnoresStaticAssets(t *testing.T) {
	js := `
fetch('/static/bundle.js')
fetch('/styles/main.css')
fetch('/images/logo.png')
axios.get('/api/v1/data')
`
	routes, err := recon.ExtractFromBody(js)
	if err != nil {
		t.Fatalf("ExtractFromBody error: %v", err)
	}

	for _, r := range routes {
		switch r.Path {
		case "/static/bundle.js", "/styles/main.css", "/images/logo.png":
			t.Errorf("static asset %q should not be extracted", r.Path)
		}
	}
}

const fixtureHTML = `<!DOCTYPE html>
<html>
<head>
  <script src="/static/vendor.js"></script>
  <script src="https://cdn.example.com/lib.js"></script>
  <script src="../app/bundle.js"></script>
</head>
<body>
  <script src="chunk.js"></script>
</body>
</html>`

func TestFindScriptURLs_AbsoluteURLs(t *testing.T) {
	base := "https://example.com/app/"

	urls, err := recon.FindScriptURLs(fixtureHTML, base)
	if err != nil {
		t.Fatalf("FindScriptURLs error: %v", err)
	}

	want := map[string]bool{
		"https://example.com/static/vendor.js": true,
		"https://cdn.example.com/lib.js":       true,
		"https://example.com/app/bundle.js":    true,
		"https://example.com/app/chunk.js":     true,
	}

	for _, u := range urls {
		if !want[u] {
			t.Errorf("unexpected URL: %q", u)
		}
		delete(want, u)
	}
	for missing := range want {
		t.Errorf("missing expected URL: %q", missing)
	}
}

func TestFindScriptURLs_Deduplication(t *testing.T) {
	html := `
<script src="/js/app.js"></script>
<script src="/js/app.js"></script>
<script src="https://example.com/js/app.js"></script>
`
	urls, err := recon.FindScriptURLs(html, "https://example.com/")
	if err != nil {
		t.Fatalf("FindScriptURLs error: %v", err)
	}

	seen := make(map[string]int)
	for _, u := range urls {
		seen[u]++
	}
	for u, count := range seen {
		if count > 1 {
			t.Errorf("URL %q appeared %d times, want 1", u, count)
		}
	}
}

func TestExtractFromURL_IntegrationWithHTTPServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `
axios.get('/api/v1/items')
axios.post('/api/v1/items')
fetch('/rest/status')
`)
	}))
	defer srv.Close()

	client := srv.Client()
	routes, err := recon.ExtractFromURL(srv.URL+"/bundle.js", client)
	if err != nil {
		t.Fatalf("ExtractFromURL error: %v", err)
	}

	byPath := make(map[string]string, len(routes))
	for _, r := range routes {
		byPath[r.Path] = r.Method
		if !hasPrefix(r.Source, "js:") {
			t.Errorf("route %q missing js: source prefix, got %q", r.Path, r.Source)
		}
	}

	if byPath["/api/v1/items"] != "POST" {
		t.Errorf("expected POST for /api/v1/items (non-GET wins), got %q", byPath["/api/v1/items"])
	}
	if _, ok := byPath["/rest/status"]; !ok {
		t.Error("/rest/status not found in extracted routes")
	}
}

func TestExtractFromBody_NoDuplicatePaths(t *testing.T) {
	js := `
fetch('/api/v1/foo')
fetch('/api/v1/foo')
fetch('/api/v1/bar')
axios.post('/api/v1/foo')
`
	routes, err := recon.ExtractFromBody(js)
	if err != nil {
		t.Fatalf("ExtractFromBody error: %v", err)
	}

	paths := make([]string, 0, len(routes))
	for _, r := range routes {
		paths = append(paths, r.Path)
	}
	sort.Strings(paths)

	for i := 1; i < len(paths); i++ {
		if paths[i] == paths[i-1] {
			t.Errorf("duplicate path %q in results", paths[i])
		}
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
