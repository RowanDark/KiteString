package wordlist

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RowanDark/kitestring/pkg/ksfile"
)

// patchSecLists overrides SecListsBaseURL, SecListsHTTPClient, and
// SecListsAliases for the duration of the test. The returned function restores
// all originals. Callers must also close srv when done.
func patchSecLists(t *testing.T, srv *httptest.Server, aliases map[string]string) func() {
	t.Helper()
	origBase := SecListsBaseURL
	origClient := SecListsHTTPClient
	origAliases := SecListsAliases

	SecListsBaseURL = srv.URL + "/"
	SecListsHTTPClient = srv.Client()
	SecListsAliases = make(map[string]string, len(aliases))
	for k, v := range aliases {
		SecListsAliases[k] = v
	}

	return func() {
		SecListsBaseURL = origBase
		SecListsHTTPClient = origClient
		SecListsAliases = origAliases
	}
}

func TestFetchSecList_ValidAlias(t *testing.T) {
	body := "/api/v1/users\n/api/v1/posts\n# comment\n\n/api/v1/health\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api-list.txt" {
			w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	restore := patchSecLists(t, srv, map[string]string{
		"test-api": "api-list.txt",
	})
	defer restore()

	routes, err := FetchSecList("test-api")
	if err != nil {
		t.Fatalf("FetchSecList: %v", err)
	}

	// 3 non-blank non-comment lines
	if len(routes) != 3 {
		t.Errorf("want 3 routes, got %d", len(routes))
	}

	for _, r := range routes {
		if r.Method != "GET" {
			t.Errorf("want GET method, got %q", r.Method)
		}
		if !strings.HasPrefix(r.Path, "/api/v1/") {
			t.Errorf("unexpected path: %q", r.Path)
		}
		if !strings.HasPrefix(r.Source, "seclists:") {
			t.Errorf("source should start with 'seclists:', got %q", r.Source)
		}
	}
}

func TestFetchSecList_UnknownAlias(t *testing.T) {
	_, err := FetchSecList("this-does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown alias, got nil")
	}
	if !strings.Contains(err.Error(), "unknown alias") {
		t.Errorf("error should mention 'unknown alias', got: %v", err)
	}
}

func TestFetchSecList_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	restore := patchSecLists(t, srv, map[string]string{
		"err-alias": "missing.txt",
	})
	defer restore()

	_, err := FetchSecList("err-alias")
	if err == nil {
		t.Fatal("expected error on HTTP 404, got nil")
	}
}

func TestFetchSecList_404ErrorMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	restore := patchSecLists(t, srv, map[string]string{
		"gone-alias": "gone.txt",
	})
	defer restore()

	_, err := FetchSecList("gone-alias")
	if err == nil {
		t.Fatal("expected error on HTTP 404, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "gone-alias") {
		t.Errorf("error should contain alias name, got: %v", msg)
	}
	if !strings.Contains(msg, "HTTP 404") {
		t.Errorf("error should mention HTTP 404, got: %v", msg)
	}
	if !strings.Contains(msg, "ks wordlist seclists list") {
		t.Errorf("error should suggest 'ks wordlist seclists list', got: %v", msg)
	}
	if !strings.Contains(msg, srv.URL) {
		t.Errorf("error should contain attempted URL, got: %v", msg)
	}
}

func TestCompileSecList_ProducesReadableKSFile(t *testing.T) {
	body := "/health\n/metrics\n/status\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	restore := patchSecLists(t, srv, map[string]string{
		"compile-test": "list.txt",
	})
	defer restore()

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "sl-compile-test.ks")

	if err := CompileSecList("compile-test", outputPath); err != nil {
		t.Fatalf("CompileSecList: %v", err)
	}

	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("output file not created: %v", err)
	}

	kf, err := ksfile.Read(outputPath)
	if err != nil {
		t.Fatalf("ksfile.Read: %v", err)
	}

	routes, err := ksfile.ToRoutes(kf)
	if err != nil {
		t.Fatalf("ksfile.ToRoutes: %v", err)
	}

	if len(routes) != 3 {
		t.Errorf("want 3 routes, got %d", len(routes))
	}

	paths := make(map[string]bool, len(routes))
	for _, r := range routes {
		paths[r.Path] = true
		if r.Method != "GET" {
			t.Errorf("want GET, got %q", r.Method)
		}
	}
	for _, p := range []string{"/health", "/metrics", "/status"} {
		if !paths[p] {
			t.Errorf("missing expected path %q", p)
		}
	}
}

func TestListSecListAliases_Sorted(t *testing.T) {
	entries := ListSecListAliases()
	if len(entries) == 0 {
		t.Fatal("expected non-empty alias list")
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].Alias < entries[i-1].Alias {
			t.Errorf("entries not sorted: %q before %q", entries[i-1].Alias, entries[i].Alias)
		}
	}
}

func TestListSecListAliases_ContainsExpected(t *testing.T) {
	entries := ListSecListAliases()
	found := make(map[string]bool, len(entries))
	for _, e := range entries {
		found[e.Alias] = true
		if e.RepoPath == "" {
			t.Errorf("alias %q has empty repo path", e.Alias)
		}
	}
	for _, expected := range []string{
		"api-endpoints", "raft-medium-words", "dirsearch",
		"combined-directories", "combined-words",
	} {
		if !found[expected] {
			t.Errorf("expected alias %q in list", expected)
		}
	}
	if found["directory-list-2.3-medium"] {
		t.Error("deprecated alias 'directory-list-2.3-medium' should not be in list")
	}
}

func TestListSecListAliases_StatusField(t *testing.T) {
	dir := t.TempDir()
	origCacheDir := CacheDirOverride
	CacheDirOverride = dir
	defer func() { CacheDirOverride = origCacheDir }()

	alias := "raft-small-words"
	cachedPath := filepath.Join(dir, "sl-"+alias+".ks")
	if err := os.WriteFile(cachedPath, []byte("sentinel"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries := ListSecListAliases()
	statusByAlias := make(map[string]string, len(entries))
	for _, e := range entries {
		statusByAlias[e.Alias] = e.Status
	}

	if statusByAlias[alias] != "ok" {
		t.Errorf("cached alias should have status 'ok', got %q", statusByAlias[alias])
	}
	if statusByAlias["api-endpoints"] != "unverified" {
		t.Errorf("uncached alias should have status 'unverified', got %q", statusByAlias["api-endpoints"])
	}
}

func TestResolveSecList_FetchesIfNotCached(t *testing.T) {
	body := "/cached-path\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	restore := patchSecLists(t, srv, map[string]string{
		"resolve-test": "list.txt",
	})
	defer restore()

	dir := t.TempDir()
	origCacheDir := CacheDirOverride
	CacheDirOverride = dir
	defer func() { CacheDirOverride = origCacheDir }()

	path, err := ResolveSecList("resolve-test")
	if err != nil {
		t.Fatalf("ResolveSecList: %v", err)
	}

	expected := filepath.Join(dir, "sl-resolve-test.ks")
	if path != expected {
		t.Errorf("want path %s, got %s", expected, path)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cached file not found: %v", err)
	}
}

func TestResolveSecList_UsesCacheIfPresent(t *testing.T) {
	dir := t.TempDir()
	origCacheDir := CacheDirOverride
	CacheDirOverride = dir
	defer func() { CacheDirOverride = origCacheDir }()

	alias := "pre-cached"
	cachedPath := filepath.Join(dir, "sl-"+alias+".ks")
	if err := os.WriteFile(cachedPath, []byte("sentinel"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Server would return an error — should never be contacted.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()

	restore := patchSecLists(t, srv, map[string]string{
		alias: "should-not-hit.txt",
	})
	defer restore()

	path, err := ResolveSecList(alias)
	if err != nil {
		t.Fatalf("ResolveSecList: %v", err)
	}
	if path != cachedPath {
		t.Errorf("want %s, got %s", cachedPath, path)
	}
}
