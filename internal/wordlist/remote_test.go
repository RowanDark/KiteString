package wordlist

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// mockManifest builds a Manifest whose download URLs point at srv.
func mockManifest(srv *httptest.Server) Manifest {
	return Manifest{
		Version:   1,
		UpdatedAt: "2026-01-01",
		Wordlists: []WordlistEntry{
			{
				Alias:            "apiroutes",
				Filename:         "apiroutes.ks",
				Description:      "Common API routes",
				Count:            215000,
				CompressedSizeMB: 4.2,
				URL:              srv.URL + "/apiroutes.ks",
			},
			{
				Alias:            "graphql",
				Filename:         "graphql.ks",
				Description:      "GraphQL paths",
				Count:            50000,
				CompressedSizeMB: 1.1,
				URL:              srv.URL + "/graphql.ks",
			},
		},
	}
}

func setupMockServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json", "/":
			m := mockManifest(srv)
			json.NewEncoder(w).Encode(m)
		case "/apiroutes.ks", "/graphql.ks":
			// Serve a minimal valid payload (not a real .ks file; tests only
			// check that the download succeeds and the file is created).
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte("KS_MOCK_PAYLOAD"))
		default:
			http.NotFound(w, r)
		}
	}))

	origURL := ManifestURL
	origClient := HTTPClient
	origCacheDir := CacheDirOverride

	ManifestURL = srv.URL + "/manifest.json"
	HTTPClient = srv.Client()

	cleanup := func() {
		ManifestURL = origURL
		HTTPClient = origClient
		CacheDirOverride = origCacheDir
		srv.Close()
	}
	return srv, cleanup
}

func TestFetchManifest_ParsesCorrectly(t *testing.T) {
	_, cleanup := setupMockServer(t)
	defer cleanup()

	m, err := FetchManifest()
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if m.Version != 1 {
		t.Errorf("want version 1, got %d", m.Version)
	}
	if m.UpdatedAt != "2026-01-01" {
		t.Errorf("want updated_at '2026-01-01', got %q", m.UpdatedAt)
	}
	if len(m.Wordlists) != 2 {
		t.Fatalf("want 2 wordlists, got %d", len(m.Wordlists))
	}
	first := m.Wordlists[0]
	if first.Alias != "apiroutes" {
		t.Errorf("want alias 'apiroutes', got %q", first.Alias)
	}
	if first.Count != 215000 {
		t.Errorf("want count 215000, got %d", first.Count)
	}
	if first.CompressedSizeMB != 4.2 {
		t.Errorf("want compressed_size_mb 4.2, got %f", first.CompressedSizeMB)
	}
}

func TestFetchManifest_NetworkFailure(t *testing.T) {
	origURL := ManifestURL
	ManifestURL = "http://127.0.0.1:1" // nothing listening
	origClient := HTTPClient
	HTTPClient = &http.Client{} // short timeout via unreachable port
	defer func() {
		ManifestURL = origURL
		HTTPClient = origClient
	}()

	_, err := FetchManifest()
	if err == nil {
		t.Fatal("expected error on network failure, got nil")
	}
}

func TestFetchManifest_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	origURL := ManifestURL
	origClient := HTTPClient
	ManifestURL = srv.URL
	HTTPClient = srv.Client()
	defer func() {
		ManifestURL = origURL
		HTTPClient = origClient
	}()

	_, err := FetchManifest()
	if err == nil {
		t.Fatal("expected error on HTTP 404, got nil")
	}
}

func TestListCached_ReadsPreseededDir(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"apiroutes.ks", "graphql.ks"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("mock"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A non-.ks file that must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}

	origCacheDir := CacheDirOverride
	CacheDirOverride = dir
	defer func() { CacheDirOverride = origCacheDir }()

	cached, err := ListCached()
	if err != nil {
		t.Fatalf("ListCached: %v", err)
	}
	if len(cached) != 2 {
		t.Fatalf("want 2 cached wordlists, got %d", len(cached))
	}
	found := make(map[string]bool, len(cached))
	for _, c := range cached {
		found[c.Alias] = true
		if c.Path != filepath.Join(dir, c.Filename) {
			t.Errorf("alias %s: want path %s, got %s", c.Alias, filepath.Join(dir, c.Filename), c.Path)
		}
	}
	for _, alias := range []string{"apiroutes", "graphql"} {
		if !found[alias] {
			t.Errorf("expected alias %q in cached list", alias)
		}
	}
}

func TestListCached_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	origCacheDir := CacheDirOverride
	CacheDirOverride = dir
	defer func() { CacheDirOverride = origCacheDir }()

	cached, err := ListCached()
	if err != nil {
		t.Fatalf("ListCached on empty dir: %v", err)
	}
	if len(cached) != 0 {
		t.Errorf("want 0 cached wordlists, got %d", len(cached))
	}
}

func TestListCached_MissingDir(t *testing.T) {
	origCacheDir := CacheDirOverride
	CacheDirOverride = filepath.Join(t.TempDir(), "nonexistent")
	defer func() { CacheDirOverride = origCacheDir }()

	cached, err := ListCached()
	if err != nil {
		t.Fatalf("ListCached on missing dir should not error: %v", err)
	}
	if cached != nil {
		t.Errorf("want nil slice for missing dir, got %v", cached)
	}
}

func TestUpdate_DownloadsSpecifiedAlias(t *testing.T) {
	_, cleanup := setupMockServer(t)
	defer cleanup()

	dir := t.TempDir()
	CacheDirOverride = dir

	if err := Update([]string{"apiroutes"}, false); err != nil {
		t.Fatalf("Update: %v", err)
	}

	dest := filepath.Join(dir, "apiroutes.ks")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("expected file at %s: %v", dest, err)
	}
	if string(data) != "KS_MOCK_PAYLOAD" {
		t.Errorf("unexpected file contents: %q", data)
	}
	// graphql.ks should not have been downloaded
	if _, err := os.Stat(filepath.Join(dir, "graphql.ks")); err == nil {
		t.Error("graphql.ks should not have been downloaded")
	}
}

func TestUpdate_SkipsCachedUnlessForce(t *testing.T) {
	_, cleanup := setupMockServer(t)
	defer cleanup()

	dir := t.TempDir()
	CacheDirOverride = dir

	// Pre-seed apiroutes.ks with sentinel content.
	sentinel := []byte("ORIGINAL")
	dest := filepath.Join(dir, "apiroutes.ks")
	if err := os.WriteFile(dest, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}

	// Without --force, the existing file should not be overwritten.
	if err := Update([]string{"apiroutes"}, false); err != nil {
		t.Fatalf("Update: %v", err)
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "ORIGINAL" {
		t.Error("pre-cached file should not be overwritten without --force")
	}

	// With --force, it should be replaced.
	if err := Update([]string{"apiroutes"}, true); err != nil {
		t.Fatalf("Update --force: %v", err)
	}
	data, _ = os.ReadFile(dest)
	if string(data) != "KS_MOCK_PAYLOAD" {
		t.Errorf("expected file overwritten with mock payload, got %q", data)
	}
}

func TestUpdate_UnknownAlias(t *testing.T) {
	_, cleanup := setupMockServer(t)
	defer cleanup()
	CacheDirOverride = t.TempDir()

	err := Update([]string{"doesnotexist"}, false)
	if err == nil {
		t.Fatal("expected error for unknown alias, got nil")
	}
}

func TestResolveAlias_WithHeadLimit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "apiroutes.ks"), []byte("mock"), 0o644); err != nil {
		t.Fatal(err)
	}

	origCacheDir := CacheDirOverride
	CacheDirOverride = dir
	defer func() { CacheDirOverride = origCacheDir }()

	path, limit, err := ResolveAlias("apiroutes:20000")
	if err != nil {
		t.Fatalf("ResolveAlias: %v", err)
	}
	if limit != 20000 {
		t.Errorf("want head limit 20000, got %d", limit)
	}
	if path != filepath.Join(dir, "apiroutes.ks") {
		t.Errorf("unexpected path: %s", path)
	}
}

func TestResolveAlias_NoLimit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "apiroutes.ks"), []byte("mock"), 0o644); err != nil {
		t.Fatal(err)
	}

	origCacheDir := CacheDirOverride
	CacheDirOverride = dir
	defer func() { CacheDirOverride = origCacheDir }()

	_, limit, err := ResolveAlias("apiroutes")
	if err != nil {
		t.Fatalf("ResolveAlias: %v", err)
	}
	if limit != 0 {
		t.Errorf("want head limit 0 (all), got %d", limit)
	}
}

func TestResolveAlias_NotCached(t *testing.T) {
	origCacheDir := CacheDirOverride
	CacheDirOverride = t.TempDir()
	defer func() { CacheDirOverride = origCacheDir }()

	_, _, err := ResolveAlias("missing")
	if err == nil {
		t.Fatal("expected error for uncached alias, got nil")
	}
}
