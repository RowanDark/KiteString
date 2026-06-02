package wordlist

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/RowanDark/kitestring/pkg/ksfile"
	"github.com/RowanDark/kitestring/pkg/proute"
)

// writeTXT writes a temporary .txt wordlist and returns its path.
func writeTXT(t *testing.T, lines []string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		fmt.Fprintln(f, l)
	}
	return f.Name()
}

// writeJSON writes a temporary .json wordlist and returns its path.
func writeJSON(t *testing.T, routes []jsonRoute) string {
	t.Helper()
	data, err := json.Marshal(routes)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(t.TempDir(), "*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

// writeKS writes a temporary .ks wordlist from routes and returns its path.
func writeKS(t *testing.T, routes []proute.Route) string {
	t.Helper()
	kf := ksfile.FromRoutes(routes, ksfile.KSFileMeta{Name: "test"})
	p := filepath.Join(t.TempDir(), "test.ks")
	if err := ksfile.Write(p, kf); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadTXT_TenPaths(t *testing.T) {
	paths := make([]string, 10)
	for i := range paths {
		paths[i] = fmt.Sprintf("/api/path/%d", i)
	}
	// Add a blank line and a comment to verify they are skipped.
	lines := append([]string{"# comment", ""}, paths...)

	p := writeTXT(t, lines)
	routes, err := LoadTXT(p)
	if err != nil {
		t.Fatalf("LoadTXT: %v", err)
	}
	if len(routes) != 10 {
		t.Fatalf("want 10 routes, got %d", len(routes))
	}
	for i, r := range routes {
		if r.Method != "GET" {
			t.Errorf("route[%d]: want method GET, got %q", i, r.Method)
		}
		if r.Path != paths[i] {
			t.Errorf("route[%d]: want path %q, got %q", i, paths[i], r.Path)
		}
	}
}

func TestLoadJSON_MethodsPreserved(t *testing.T) {
	input := []jsonRoute{
		{Method: "GET", Path: "/api/users"},
		{Method: "POST", Path: "/api/users"},
		{Method: "DELETE", Path: "/api/users/1"},
		{Method: "", Path: "/api/items"}, // empty method → GET
	}
	p := writeJSON(t, input)
	routes, err := LoadJSON(p)
	if err != nil {
		t.Fatalf("LoadJSON: %v", err)
	}
	if len(routes) != 4 {
		t.Fatalf("want 4 routes, got %d", len(routes))
	}
	want := []string{"GET", "POST", "DELETE", "GET"}
	for i, r := range routes {
		if r.Method != want[i] {
			t.Errorf("route[%d]: want method %q, got %q", i, want[i], r.Method)
		}
	}
}

func TestLoadJSON_CrumbTypesPreserved(t *testing.T) {
	input := []jsonRoute{
		{
			Method: "POST",
			Path:   "/api/v1/users",
			Headers: []jsonCrumb{
				{Key: "Content-Type", Type: "string", Example: "application/json"},
			},
			BodyParams: []jsonCrumb{
				{Key: "email", Type: "email", Required: true},
				{Key: "id", Type: "uuid"},
				{Key: "count", Type: "int"},
			},
		},
	}
	p := writeJSON(t, input)
	routes, err := LoadJSON(p)
	if err != nil {
		t.Fatalf("LoadJSON: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("want 1 route, got %d", len(routes))
	}
	r := routes[0]
	if len(r.Headers) != 1 || r.Headers[0].Type != proute.CrumbString {
		t.Errorf("unexpected header crumb: %+v", r.Headers)
	}
	if len(r.BodyParams) != 3 {
		t.Fatalf("want 3 body params, got %d", len(r.BodyParams))
	}
	if r.BodyParams[0].Type != proute.CrumbEmail || !r.BodyParams[0].Required {
		t.Errorf("body[0] crumb: %+v", r.BodyParams[0])
	}
	if r.BodyParams[1].Type != proute.CrumbUUID {
		t.Errorf("body[1] crumb: %+v", r.BodyParams[1])
	}
	if r.BodyParams[2].Type != proute.CrumbInt {
		t.Errorf("body[2] crumb: %+v", r.BodyParams[2])
	}
}

func TestLoadKS_RoundTrip(t *testing.T) {
	original := make([]proute.Route, 5)
	for i := range original {
		original[i] = proute.Route{
			Method: "POST",
			Path:   fmt.Sprintf("/api/v1/item/%d", i),
			Headers: []proute.Crumb{
				{Key: "Authorization", Type: proute.CrumbString, Required: true, Example: "Bearer token"},
			},
			BodyParams: []proute.Crumb{
				{Key: "email", Type: proute.CrumbEmail, Required: true},
			},
		}
	}

	p := writeKS(t, original)
	routes, err := LoadKS(p)
	if err != nil {
		t.Fatalf("LoadKS: %v", err)
	}
	if len(routes) != len(original) {
		t.Fatalf("want %d routes, got %d", len(original), len(routes))
	}
	for i, r := range routes {
		if r.Method != original[i].Method || r.Path != original[i].Path {
			t.Errorf("route[%d]: got %q %q, want %q %q", i, r.Method, r.Path, original[i].Method, original[i].Path)
		}
	}
}

func TestLoad_MergeAndDedup(t *testing.T) {
	// .txt file: 5 GET routes
	txtPaths := []string{
		"/api/a",
		"/api/b",
		"/api/c",
		"/api/d",
		"/api/e",
	}
	txtFile := writeTXT(t, txtPaths)

	// .json file: 3 routes, 2 of which duplicate the .txt entries
	jsonInput := []jsonRoute{
		{Method: "GET", Path: "/api/a"},  // duplicate
		{Method: "GET", Path: "/api/e"},  // duplicate
		{Method: "POST", Path: "/api/f"}, // new (different method+path)
	}
	jsonFile := writeJSON(t, jsonInput)

	routes, err := Load([]string{txtFile, jsonFile})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// 5 from txt + 1 new from json = 6 unique routes
	if len(routes) != 6 {
		t.Fatalf("want 6 routes after dedup, got %d", len(routes))
	}
}

func TestLoad_Dedup_SameMethodDifferentPath(t *testing.T) {
	lines := []string{"/a", "/b", "/c"}
	p1 := writeTXT(t, lines)
	p2 := writeTXT(t, lines) // identical content

	routes, err := Load([]string{p1, p2})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(routes) != 3 {
		t.Fatalf("want 3 routes, got %d", len(routes))
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load([]string{"/nonexistent/path/wordlist.txt"})
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_UnsupportedExtension(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.csv")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	_, err = Load([]string{f.Name()})
	if err == nil {
		t.Fatal("expected error for unsupported extension, got nil")
	}
}

func TestLoad_MalformedJSON(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.json")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(f, `{not valid json`)
	f.Close()

	_, err = Load([]string{f.Name()})
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestHead_LimitsPerFile(t *testing.T) {
	paths := make([]string, 20)
	for i := range paths {
		paths[i] = fmt.Sprintf("/path/%d", i)
	}
	p := writeTXT(t, paths)

	routes, err := Head([]string{p}, 10)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if len(routes) != 10 {
		t.Fatalf("want 10 routes, got %d", len(routes))
	}
}

func TestHead_ZeroMeansAll(t *testing.T) {
	paths := make([]string, 15)
	for i := range paths {
		paths[i] = fmt.Sprintf("/path/%d", i)
	}
	p := writeTXT(t, paths)

	routes, err := Head([]string{p}, 0)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if len(routes) != 15 {
		t.Fatalf("want 15 routes, got %d", len(routes))
	}
}
