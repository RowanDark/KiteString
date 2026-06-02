package brute

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RowanDark/kitestring/pkg/proute"
)

// --- paths.go tests ---

func TestExpandExtensions(t *testing.T) {
	got := ExpandExtensions([]string{"admin"}, []string{"php", "json"})
	want := []string{"admin.php", "admin.json"}
	if !sliceEqual(got, want) {
		t.Errorf("ExpandExtensions = %v, want %v", got, want)
	}
}

func TestExpandExtensions_EmptyExtensions(t *testing.T) {
	paths := []string{"admin", "login"}
	got := ExpandExtensions(paths, nil)
	if !sliceEqual(got, paths) {
		t.Errorf("ExpandExtensions with no extensions should return input unchanged, got %v", got)
	}
}

func TestExpandExtensions_DotPrefix(t *testing.T) {
	got := ExpandExtensions([]string{"index"}, []string{".html", "php"})
	want := []string{"index.html", "index.php"}
	if !sliceEqual(got, want) {
		t.Errorf("ExpandExtensions = %v, want %v", got, want)
	}
}

func TestExpandDirsearch(t *testing.T) {
	got := ExpandDirsearch([]string{"/page.%EXT%"}, []string{"html", "php"})
	want := []string{"/page.html", "/page.php"}
	if !sliceEqual(got, want) {
		t.Errorf("ExpandDirsearch = %v, want %v", got, want)
	}
}

func TestExpandDirsearch_NoPlaceholder(t *testing.T) {
	got := ExpandDirsearch([]string{"/admin", "/login"}, []string{"php", "html"})
	want := []string{"/admin", "/login"}
	if !sliceEqual(got, want) {
		t.Errorf("ExpandDirsearch with no %%EXT%% should pass through unchanged, got %v", got)
	}
}

func TestExpandDirsearch_Mixed(t *testing.T) {
	paths := []string{"/page.%EXT%", "/static"}
	got := ExpandDirsearch(paths, []string{"html"})
	want := []string{"/page.html", "/static"}
	if !sliceEqual(got, want) {
		t.Errorf("ExpandDirsearch = %v, want %v", got, want)
	}
}

func TestDeduplicate(t *testing.T) {
	got := Deduplicate([]string{"a", "b", "a", "c", "b"})
	want := []string{"a", "b", "c"}
	if !sliceEqual(got, want) {
		t.Errorf("Deduplicate = %v, want %v", got, want)
	}
}

func TestDeduplicate_NoDupes(t *testing.T) {
	paths := []string{"x", "y", "z"}
	got := Deduplicate(paths)
	if !sliceEqual(got, paths) {
		t.Errorf("Deduplicate on unique paths = %v, want %v", got, paths)
	}
}

// --- bruter integration tests ---

func TestBruter_FindsValidPaths(t *testing.T) {
	validPaths := map[string]bool{
		"/admin":    true,
		"/login":    true,
		"/api":      true,
		"/users":    true,
		"/settings": true,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if validPaths[r.URL.Path] {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "found")
		} else {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "not found")
		}
	}))
	defer srv.Close()

	targets, err := proute.ParseTarget(srv.URL)
	if err != nil {
		t.Fatalf("ParseTarget: %v", err)
	}

	paths := []string{"/admin", "/login", "/api", "/users", "/settings", "/missing", "/nope"}

	cfg := proute.ScanConfig{
		MaxConnPerHost:   5,
		MaxParallelHosts: 1,
		FailStatusCodes:  []int{404},
		DisablePreflight: true,
	}

	b, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var buf strings.Builder
	b.SetOutput(&buf)

	if err := b.Run(targets, paths); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if b.ResultCount() != int64(len(validPaths)) {
		t.Errorf("ResultCount = %d, want %d", b.ResultCount(), len(validPaths))
	}
}

func TestBruter_ExtensionExpansion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin.php" || r.URL.Path == "/admin.html" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	targets, err := proute.ParseTarget(srv.URL)
	if err != nil {
		t.Fatalf("ParseTarget: %v", err)
	}

	paths := Deduplicate(ExpandExtensions([]string{"admin"}, []string{"php", "html", "aspx"}))

	cfg := proute.ScanConfig{
		MaxConnPerHost:   5,
		MaxParallelHosts: 1,
		FailStatusCodes:  []int{404},
		DisablePreflight: true,
	}

	b, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var buf strings.Builder
	b.SetOutput(&buf)

	if err := b.Run(targets, paths); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if b.ResultCount() != 2 {
		t.Errorf("ResultCount = %d, want 2", b.ResultCount())
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
