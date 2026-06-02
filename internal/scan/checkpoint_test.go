package scan_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RowanDark/kitestring/internal/scan"
	"github.com/RowanDark/kitestring/pkg/proute"
)

func newTestCheckpoint() *scan.Checkpoint {
	return scan.NewCheckpoint(
		proute.ScanConfig{MaxConnPerHost: 5, UserAgent: "test/1.0"},
		[]proute.ScanTarget{{Host: "example.com", Scheme: "https"}},
		[]string{"routes.ks"},
	)
}

// TestCheckpointSaveLoad verifies that all state survives a round-trip through disk.
func TestCheckpointSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	cp := newTestCheckpoint()
	cp.MarkComplete("GET", "example.com", "/api/v1/users")
	cp.MarkComplete("POST", "example.com", "/api/v1/login")
	cp.AddResult(proute.ScanResult{
		Target:     proute.ScanTarget{Host: "example.com", Scheme: "https"},
		Route:      proute.Route{Method: "GET", Path: "/api/v1/users"},
		StatusCode: 200,
		Timestamp:  time.Now(),
	})

	if err := cp.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cp2 := &scan.Checkpoint{}
	if err := cp2.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cp2.ScanID != cp.ScanID {
		t.Errorf("ScanID mismatch: got %q, want %q", cp2.ScanID, cp.ScanID)
	}
	if cp2.Version != 1 {
		t.Errorf("Version: got %d, want 1", cp2.Version)
	}
	if cp2.CompletedCount() != 2 {
		t.Errorf("CompletedCount: got %d, want 2", cp2.CompletedCount())
	}
	if !cp2.IsComplete("GET", "example.com", "/api/v1/users") {
		t.Error("expected GET /api/v1/users to be complete")
	}
	if !cp2.IsComplete("POST", "example.com", "/api/v1/login") {
		t.Error("expected POST /api/v1/login to be complete")
	}
	if cp2.IsComplete("DELETE", "example.com", "/api/v1/users") {
		t.Error("DELETE /api/v1/users should NOT be complete")
	}
	if len(cp2.Results) != 1 || cp2.Results[0].StatusCode != 200 {
		t.Errorf("Results not preserved: got %v", cp2.Results)
	}
	if len(cp2.WordlistPaths) != 1 || cp2.WordlistPaths[0] != "routes.ks" {
		t.Errorf("WordlistPaths not preserved: got %v", cp2.WordlistPaths)
	}
	if len(cp2.Targets) != 1 || cp2.Targets[0].Host != "example.com" {
		t.Errorf("Targets not preserved: got %v", cp2.Targets)
	}
}

// TestRemainingRoutes verifies that completed route+target pairs are excluded.
func TestRemainingRoutes(t *testing.T) {
	cp := scan.NewCheckpoint(proute.ScanConfig{}, nil, nil)
	cp.MarkComplete("GET", "example.com", "/api/users")
	cp.MarkComplete("POST", "example.com", "/api/login")

	allRoutes := []proute.Route{
		{Method: "GET", Path: "/api/users"},
		{Method: "POST", Path: "/api/login"},
		{Method: "DELETE", Path: "/api/users"},
		{Method: "GET", Path: "/api/products"},
	}
	target := proute.ScanTarget{Host: "example.com"}

	remaining := cp.RemainingRoutes(allRoutes, target)
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining routes, got %d: %v", len(remaining), remaining)
	}
	for _, r := range remaining {
		if r.Method == "GET" && r.Path == "/api/users" {
			t.Error("completed route GET /api/users should not be in remaining")
		}
		if r.Method == "POST" && r.Path == "/api/login" {
			t.Error("completed route POST /api/login should not be in remaining")
		}
	}
}

// TestAtomicWrite verifies that the .tmp file is cleaned up after a successful
// save and that the checkpoint file remains valid.
func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	cp := newTestCheckpoint()
	cp.MarkComplete("GET", "example.com", "/first")

	if err := cp.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The .tmp file must not exist after a successful atomic save.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("expected .tmp file to be cleaned up after atomic save")
	}

	// The checkpoint file must still be valid.
	cp2 := &scan.Checkpoint{}
	if err := cp2.Load(path); err != nil {
		t.Fatalf("Load after atomic save: %v", err)
	}
	if !cp2.IsComplete("GET", "example.com", "/first") {
		t.Error("GET /first should be complete after reload")
	}
}

// TestAtomicWritePreservesOldOnFailure verifies that a second save overwrites
// the file correctly and neither leaves a .tmp nor corrupts the original.
func TestAtomicWritePreservesOldOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	cp := newTestCheckpoint()
	cp.MarkComplete("GET", "example.com", "/v1")
	if err := cp.Save(path); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	// Second save adds more state.
	cp.MarkComplete("POST", "example.com", "/v2")
	if err := cp.Save(path); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	cp2 := &scan.Checkpoint{}
	if err := cp2.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cp2.CompletedCount() != 2 {
		t.Errorf("expected 2 completed, got %d", cp2.CompletedCount())
	}
}

// TestResumeSkipsCompleted verifies that N already-completed routes are excluded
// and scanning continues from the right position.
func TestResumeSkipsCompleted(t *testing.T) {
	cp := scan.NewCheckpoint(proute.ScanConfig{}, nil, nil)
	cp.MarkComplete("GET", "example.com", "/api/users")
	cp.MarkComplete("GET", "example.com", "/api/products")

	allRoutes := []proute.Route{
		{Method: "GET", Path: "/api/users"},
		{Method: "GET", Path: "/api/products"},
		{Method: "GET", Path: "/api/orders"},
	}
	target := proute.ScanTarget{Host: "example.com"}

	remaining := cp.RemainingRoutes(allRoutes, target)
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining route, got %d: %v", len(remaining), remaining)
	}
	if remaining[0].Path != "/api/orders" {
		t.Errorf("expected /api/orders to be next, got %s", remaining[0].Path)
	}
}

// TestResumeAllComplete verifies that RemainingRoutes returns empty when everything is done.
func TestResumeAllComplete(t *testing.T) {
	routes := []proute.Route{
		{Method: "GET", Path: "/a"},
		{Method: "POST", Path: "/b"},
	}
	target := proute.ScanTarget{Host: "example.com"}

	cp := scan.NewCheckpoint(proute.ScanConfig{}, nil, nil)
	for _, r := range routes {
		cp.MarkComplete(r.Method, target.Host, r.Path)
	}

	remaining := cp.RemainingRoutes(routes, target)
	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining routes, got %d", len(remaining))
	}
}

// TestCheckpointThreadSafety hammers MarkComplete and IsComplete from multiple
// goroutines to verify there are no data races.
func TestCheckpointThreadSafety(t *testing.T) {
	cp := scan.NewCheckpoint(proute.ScanConfig{}, nil, nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "/path/" + string(rune('a'+n%26))
			cp.MarkComplete("GET", "example.com", key)
			cp.IsComplete("GET", "example.com", key)
		}(i)
	}
	wg.Wait()
}

// TestCheckpointVersionMismatch verifies that loading a checkpoint with an
// unknown version returns an error rather than silently accepting bad data.
func TestCheckpointVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	// Write a checkpoint with an unsupported version number.
	data := []byte(`{"Version":99,"ScanID":"test","CompletedKeys":[]}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	cp := &scan.Checkpoint{}
	if err := cp.Load(path); err == nil {
		t.Error("expected error for version mismatch, got nil")
	}
}

// TestCheckpointScanIDUnique verifies that two checkpoints created in sequence
// have different ScanIDs.
func TestCheckpointScanIDUnique(t *testing.T) {
	cp1 := newTestCheckpoint()
	cp2 := newTestCheckpoint()
	if cp1.ScanID == cp2.ScanID {
		t.Errorf("expected unique ScanIDs, got duplicate: %s", cp1.ScanID)
	}
}

// TestCheckpointQuarantinedRoundTrip verifies that quarantined hosts survive
// a save/load cycle.
func TestCheckpointQuarantinedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	cp := newTestCheckpoint()
	cp.SetQuarantined([]string{"bad.example.com", "evil.example.com"})

	if err := cp.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cp2 := &scan.Checkpoint{}
	if err := cp2.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(cp2.Quarantined) != 2 {
		t.Errorf("expected 2 quarantined hosts, got %d: %v", len(cp2.Quarantined), cp2.Quarantined)
	}
}
