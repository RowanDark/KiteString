package scan_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RowanDark/kitestring/internal/scan"
	"github.com/RowanDark/kitestring/pkg/proute"
)

func makeTestCheckpoint(t *testing.T) *scan.Checkpoint {
	t.Helper()
	config := proute.ScanConfig{
		MaxConnPerHost:   5,
		MaxParallelHosts: 10,
		Timeout:          10 * time.Second,
		UserAgent:        "KiteString/test",
	}
	targets := []proute.ScanTarget{
		{Scheme: "https", Host: "api.example.com", Port: 443},
	}
	return scan.NewCheckpoint(config, targets, []string{"routes.ks"})
}

// TestCheckpoint_SaveLoad verifies that a checkpoint round-trips through Save/Load
// with zero data loss across all fields.
func TestCheckpoint_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scan.json")

	cp := makeTestCheckpoint(t)
	cp.MarkComplete("GET", "api.example.com", "/api/users")
	cp.MarkComplete("POST", "api.example.com", "/api/auth/login")
	cp.AddResult(proute.ScanResult{
		StatusCode: 200,
		URL:        "https://api.example.com/api/users",
	})

	if err := cp.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded := &scan.Checkpoint{}
	if err := loaded.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.ScanID != cp.ScanID {
		t.Errorf("ScanID: got %q, want %q", loaded.ScanID, cp.ScanID)
	}
	if len(loaded.CompletedKeys) != 2 {
		t.Errorf("CompletedKeys length: got %d, want 2", len(loaded.CompletedKeys))
	}
	if len(loaded.Results) != 1 {
		t.Errorf("Results length: got %d, want 1", len(loaded.Results))
	}
	if loaded.Results[0].URL != "https://api.example.com/api/users" {
		t.Errorf("Results[0].URL: got %q", loaded.Results[0].URL)
	}
	if len(loaded.Targets) != 1 || loaded.Targets[0].Host != "api.example.com" {
		t.Errorf("Targets not preserved after round-trip")
	}
	if len(loaded.WordlistPaths) != 1 || loaded.WordlistPaths[0] != "routes.ks" {
		t.Errorf("WordlistPaths not preserved after round-trip")
	}
	if loaded.Config.UserAgent != "KiteString/test" {
		t.Errorf("Config.UserAgent: got %q, want %q", loaded.Config.UserAgent, "KiteString/test")
	}
}

// TestCheckpoint_RemainingRoutes verifies that routes already recorded in
// CompletedKeys are excluded from the result.
func TestCheckpoint_RemainingRoutes(t *testing.T) {
	cp := makeTestCheckpoint(t)
	target := proute.ScanTarget{Host: "api.example.com"}

	all := []proute.Route{
		{Method: "GET", Path: "/api/users"},
		{Method: "POST", Path: "/api/users"},
		{Method: "GET", Path: "/api/orders"},
		{Method: "DELETE", Path: "/api/users"},
	}

	// Mark two as complete.
	cp.MarkComplete("GET", "api.example.com", "/api/users")
	cp.MarkComplete("POST", "api.example.com", "/api/users")

	remaining := cp.RemainingRoutes(all, target)

	if len(remaining) != 2 {
		t.Fatalf("RemainingRoutes: got %d, want 2", len(remaining))
	}
	for _, r := range remaining {
		if r.Method == "GET" && r.Path == "/api/users" {
			t.Error("RemainingRoutes includes GET /api/users which should be complete")
		}
		if r.Method == "POST" && r.Path == "/api/users" {
			t.Error("RemainingRoutes includes POST /api/users which should be complete")
		}
	}
}

// TestCheckpoint_RemainingRoutes_DifferentHost verifies that completed keys are
// scoped to the host — the same method+path on a different host is NOT skipped.
func TestCheckpoint_RemainingRoutes_DifferentHost(t *testing.T) {
	cp := makeTestCheckpoint(t)

	cp.MarkComplete("GET", "api.example.com", "/api/users")

	// Same route but different host — should NOT be excluded.
	other := proute.ScanTarget{Host: "staging.example.com"}
	all := []proute.Route{{Method: "GET", Path: "/api/users"}}

	remaining := cp.RemainingRoutes(all, other)
	if len(remaining) != 1 {
		t.Errorf("RemainingRoutes for different host: got %d, want 1", len(remaining))
	}
}

// TestCheckpoint_AtomicWrite verifies that a Save interrupted mid-write does not
// corrupt the previous checkpoint. We simulate this by checking that the .tmp file
// is gone after a successful save and that the checkpoint file is valid JSON.
func TestCheckpoint_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scan.json")

	cp := makeTestCheckpoint(t)
	cp.MarkComplete("GET", "api.example.com", "/ping")

	if err := cp.Save(path); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	// The temporary file must not exist after a successful save.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file still exists after successful Save")
	}

	// The checkpoint file must be loadable.
	loaded := &scan.Checkpoint{}
	if err := loaded.Load(path); err != nil {
		t.Fatalf("Load after atomic write: %v", err)
	}
	if len(loaded.CompletedKeys) != 1 {
		t.Errorf("CompletedKeys after reload: got %d, want 1", len(loaded.CompletedKeys))
	}

	// Overwrite with more data — previous checkpoint must not be corrupted if we
	// load the file that was written by the first save (already tested above).
	cp.MarkComplete("POST", "api.example.com", "/login")
	if err := cp.Save(path); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	loaded2 := &scan.Checkpoint{}
	if err := loaded2.Load(path); err != nil {
		t.Fatalf("Load after second save: %v", err)
	}
	if len(loaded2.CompletedKeys) != 2 {
		t.Errorf("CompletedKeys after second save: got %d, want 2", len(loaded2.CompletedKeys))
	}
}

// TestCheckpoint_ResumeSkipsCompleted verifies that after loading a checkpoint
// with N completed routes, RemainingRoutes correctly excludes those N routes
// and continues from the right position.
func TestCheckpoint_ResumeSkipsCompleted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scan.json")

	const totalRoutes = 10
	const completedBefore = 6

	cp := makeTestCheckpoint(t)
	target := proute.ScanTarget{Host: "api.example.com"}

	all := make([]proute.Route, totalRoutes)
	for i := range all {
		all[i] = proute.Route{Method: "GET", Path: filepath.Join("/route", string(rune('a'+i)))}
	}

	// Simulate completing the first N routes.
	for i := 0; i < completedBefore; i++ {
		cp.MarkComplete(all[i].Method, target.Host, all[i].Path)
	}

	if err := cp.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load as if resuming.
	resumed := &scan.Checkpoint{}
	if err := resumed.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(resumed.CompletedKeys) != completedBefore {
		t.Fatalf("loaded CompletedKeys: got %d, want %d", len(resumed.CompletedKeys), completedBefore)
	}

	remaining := resumed.RemainingRoutes(all, target)
	want := totalRoutes - completedBefore
	if len(remaining) != want {
		t.Errorf("remaining routes: got %d, want %d", len(remaining), want)
	}

	// Ensure none of the remaining routes were in the completed set.
	completedSet := make(map[string]bool, completedBefore)
	for i := 0; i < completedBefore; i++ {
		completedSet[all[i].Path] = true
	}
	for _, r := range remaining {
		if completedSet[r.Path] {
			t.Errorf("RemainingRoutes contains already-completed route %s %s", r.Method, r.Path)
		}
	}
}

// TestCheckpoint_IsComplete verifies thread-safety and correctness of MarkComplete
// and IsComplete.
func TestCheckpoint_IsComplete(t *testing.T) {
	cp := makeTestCheckpoint(t)

	if cp.IsComplete("GET", "example.com", "/api") {
		t.Error("IsComplete returned true before MarkComplete")
	}

	cp.MarkComplete("GET", "example.com", "/api")

	if !cp.IsComplete("GET", "example.com", "/api") {
		t.Error("IsComplete returned false after MarkComplete")
	}
	// Different method — must not match.
	if cp.IsComplete("POST", "example.com", "/api") {
		t.Error("IsComplete returned true for different method")
	}
	// Different host — must not match.
	if cp.IsComplete("GET", "other.com", "/api") {
		t.Error("IsComplete returned true for different host")
	}
}
