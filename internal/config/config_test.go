package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RowanDark/kitestring/internal/config"
)

// TestLoadFixture verifies that Load correctly parses a two-profile YAML config.
func TestLoadFixture(t *testing.T) {
	cfg, err := config.Load("testdata/fixture.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Version != 1 {
		t.Errorf("version: got %d, want 1", cfg.Version)
	}
	if cfg.Defaults.MaxConnPerHost != 5 {
		t.Errorf("defaults.max_conn_per_host: got %d, want 5", cfg.Defaults.MaxConnPerHost)
	}
	if cfg.Defaults.Timeout.Duration != 3*time.Second {
		t.Errorf("defaults.timeout: got %v, want 3s", cfg.Defaults.Timeout.Duration)
	}

	names := cfg.ListProfiles()
	if len(names) != 2 {
		t.Fatalf("profile count: got %d, want 2", len(names))
	}
	// ListProfiles returns sorted names.
	if names[0] != "bugcrowd-generic" || names[1] != "hackerone-stripe" {
		t.Errorf("profile names: got %v", names)
	}

	stripe, ok := cfg.Profiles["hackerone-stripe"]
	if !ok {
		t.Fatal("hackerone-stripe profile missing")
	}
	if stripe.MaxConnPerHost == nil || *stripe.MaxConnPerHost != 3 {
		t.Errorf("stripe.max_conn_per_host: want 3")
	}
	if stripe.Output != "jsonl" {
		t.Errorf("stripe.output: got %q, want jsonl", stripe.Output)
	}
	if stripe.OpenAPIURL == "" {
		t.Error("stripe.openapi_url should not be empty")
	}
	if len(stripe.Wordlists) != 2 {
		t.Errorf("stripe wordlist count: got %d, want 2", len(stripe.Wordlists))
	}

	bugcrowd, ok := cfg.Profiles["bugcrowd-generic"]
	if !ok {
		t.Fatal("bugcrowd-generic profile missing")
	}
	if bugcrowd.MaxConnPerHost == nil || *bugcrowd.MaxConnPerHost != 2 {
		t.Errorf("bugcrowd.max_conn_per_host: want 2")
	}
	if bugcrowd.Delay == nil || bugcrowd.Delay.Duration != 500*time.Millisecond {
		t.Errorf("bugcrowd.delay: want 500ms")
	}
}

// TestApplyProfileMerge verifies that profile values override defaults correctly.
func TestApplyProfileMerge(t *testing.T) {
	cfg, err := config.Load("testdata/fixture.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// hackerone-stripe overrides: max_conn_per_host, delay, fail_status_codes, output, report, wordlists.
	pc, err := cfg.ApplyProfile("hackerone-stripe")
	if err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}

	// Profile-overridden values.
	if pc.MaxConnPerHost != 3 {
		t.Errorf("MaxConnPerHost: got %d, want 3", pc.MaxConnPerHost)
	}
	if pc.Delay != 200*time.Millisecond {
		t.Errorf("Delay: got %v, want 200ms", pc.Delay)
	}
	if pc.Output != "jsonl" {
		t.Errorf("Output: got %q, want jsonl", pc.Output)
	}
	if pc.Report != "md" {
		t.Errorf("Report: got %q, want md", pc.Report)
	}
	if len(pc.FailStatusCodes) != 3 {
		t.Errorf("FailStatusCodes len: got %d, want 3", len(pc.FailStatusCodes))
	}
	if len(pc.Wordlists) != 2 {
		t.Errorf("Wordlists len: got %d, want 2", len(pc.Wordlists))
	}

	// Defaults carried over: timeout (not overridden in stripe profile).
	if pc.Timeout != 3*time.Second {
		t.Errorf("Timeout (from defaults): got %v, want 3s", pc.Timeout)
	}
	// MaxParallelHosts not overridden → defaults value (50).
	if pc.MaxParallelHosts != 50 {
		t.Errorf("MaxParallelHosts (from defaults): got %d, want 50", pc.MaxParallelHosts)
	}

	// bugcrowd-generic: only overrides conn/delay/wordlists; everything else from defaults.
	bg, err := cfg.ApplyProfile("bugcrowd-generic")
	if err != nil {
		t.Fatalf("ApplyProfile bugcrowd: %v", err)
	}
	if bg.MaxConnPerHost != 2 {
		t.Errorf("bugcrowd MaxConnPerHost: got %d, want 2", bg.MaxConnPerHost)
	}
	// Output not overridden → defaults value ("pretty").
	if bg.Output != "pretty" {
		t.Errorf("bugcrowd Output (from defaults): got %q, want pretty", bg.Output)
	}
	// SimilarityThreshold not overridden → defaults value (0.85).
	if bg.SimilarityThreshold != 0.85 {
		t.Errorf("bugcrowd SimilarityThreshold: got %v, want 0.85", bg.SimilarityThreshold)
	}
}

// TestCLIFlagOverridesProfile simulates the CLI-override pattern: the caller takes
// the ProbeConfig and then replaces fields where the user explicitly set a flag.
// This mirrors what the scan/brute command handlers do via cmd.Flags().Changed().
func TestCLIFlagOverridesProfile(t *testing.T) {
	cfg, err := config.Load("testdata/fixture.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	pc, err := cfg.ApplyProfile("hackerone-stripe")
	if err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}

	// Profile sets MaxConnPerHost=3. Simulate user passing --threads 10 on CLI.
	cliThreads := 10
	flagChanged := true // simulates cmd.Flags().Changed("threads")
	if flagChanged {
		pc.MaxConnPerHost = cliThreads
	}

	if pc.MaxConnPerHost != 10 {
		t.Errorf("after CLI override, MaxConnPerHost: got %d, want 10", pc.MaxConnPerHost)
	}
	// Other profile values should remain untouched.
	if pc.Output != "jsonl" {
		t.Errorf("Output should still be from profile: got %q", pc.Output)
	}
}

// TestLoadMalformedYAML verifies that Load returns a clear error for invalid YAML.
func TestLoadMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("version: [\nbadyaml: {unclosed"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(bad)
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
	if !strings.Contains(err.Error(), "parsing config file") {
		t.Errorf("error should mention 'parsing config file', got: %v", err)
	}
}

// TestTildeExpansion verifies that ~ in path fields is expanded to the home directory.
func TestTildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load("testdata/fixture.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Default wordlist should have ~ expanded.
	if len(cfg.Defaults.Wordlists) == 0 {
		t.Fatal("expected at least one default wordlist")
	}
	for _, wl := range cfg.Defaults.Wordlists {
		if strings.HasPrefix(wl, "~") {
			t.Errorf("default wordlist path still contains ~: %s", wl)
		}
		if !strings.HasPrefix(wl, home) {
			t.Errorf("default wordlist path not expanded: %s (home=%s)", wl, home)
		}
	}

	// Profile scope_file should have ~ expanded.
	stripe := cfg.Profiles["hackerone-stripe"]
	if strings.HasPrefix(stripe.ScopeFile, "~") {
		t.Errorf("stripe scope_file still contains ~: %s", stripe.ScopeFile)
	}
	if !strings.HasPrefix(stripe.ScopeFile, home) {
		t.Errorf("stripe scope_file not expanded: %s", stripe.ScopeFile)
	}

	// Profile wordlists should have ~ expanded.
	for _, wl := range stripe.Wordlists {
		if strings.HasPrefix(wl, "~") {
			t.Errorf("stripe wordlist still contains ~: %s", wl)
		}
	}
}

// TestApplyProfileNotFound verifies a clear error when the profile doesn't exist.
func TestApplyProfileNotFound(t *testing.T) {
	cfg := config.Default()
	_, err := cfg.ApplyProfile("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention profile name, got: %v", err)
	}
}

// TestListProfilesSorted verifies that ListProfiles returns names in sorted order.
func TestListProfilesSorted(t *testing.T) {
	cfg, err := config.Load("testdata/fixture.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	names := cfg.ListProfiles()
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("ListProfiles not sorted at index %d: %v", i, names)
		}
	}
}
