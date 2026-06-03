package scope_test

import (
	"os"
	"testing"

	"github.com/RowanDark/kitestring/internal/scope"
	"github.com/RowanDark/kitestring/pkg/proute"
)

// build creates a Scope from inline include and exclude patterns.
func build(t *testing.T, includes, excludes []string) *scope.Scope {
	t.Helper()
	s := scope.New()
	for _, p := range includes {
		if err := s.AddInclude(p); err != nil {
			t.Fatalf("AddInclude(%q): %v", p, err)
		}
	}
	for _, p := range excludes {
		if err := s.AddExclude(p); err != nil {
			t.Fatalf("AddExclude(%q): %v", p, err)
		}
	}
	return s
}

// --- Required unit tests from issue ---

func TestIsInScope_WildcardIncludeReturnsTrue(t *testing.T) {
	s := build(t, []string{"*.example.com"}, nil)
	if !s.IsInScope("api.example.com") {
		t.Error("expected api.example.com to be in scope with *.example.com include")
	}
}

func TestIsInScope_ExcludeOverridesInclude(t *testing.T) {
	s := build(t, []string{"*.example.com"}, []string{"staging.example.com"})
	if s.IsInScope("staging.example.com") {
		t.Error("expected staging.example.com to be out of scope when explicitly excluded")
	}
}

func TestIsInScope_NoMatchingIncludeReturnsFalse(t *testing.T) {
	s := build(t, []string{"*.example.com"}, nil)
	if s.IsInScope("api.other.com") {
		t.Error("expected api.other.com to be out of scope — no matching include pattern")
	}
}

func TestIsInScope_CIDRAcceptsInRange(t *testing.T) {
	s := build(t, []string{"192.168.1.0/24"}, nil)
	if !s.IsInScope("192.168.1.1") {
		t.Error("expected 192.168.1.1 to be in scope for 192.168.1.0/24")
	}
	if !s.IsInScope("192.168.1.254") {
		t.Error("expected 192.168.1.254 to be in scope for 192.168.1.0/24")
	}
}

func TestIsInScope_CIDRRejectsOutOfRange(t *testing.T) {
	s := build(t, []string{"192.168.1.0/24"}, nil)
	if s.IsInScope("192.168.2.1") {
		t.Error("expected 192.168.2.1 to be out of scope for 192.168.1.0/24")
	}
	if s.IsInScope("10.0.0.1") {
		t.Error("expected 10.0.0.1 to be out of scope for 192.168.1.0/24")
	}
}

func TestFilterTargets_InScopeCountAndSkipped(t *testing.T) {
	s := build(t, []string{"*.example.com"}, nil)
	targets := []proute.ScanTarget{
		{Host: "api.example.com"},
		{Host: "staging.example.com"},
		{Host: "other.com"},
		{Host: "evil.internal"},
	}
	inScope, skipped := s.FilterTargets(targets)
	if len(inScope) != 2 {
		t.Errorf("expected 2 in-scope targets, got %d", len(inScope))
	}
	if skipped != 2 {
		t.Errorf("expected 2 skipped targets, got %d", skipped)
	}
}

// --- Additional coverage ---

func TestIsInScope_NilScopeAllowsAll(t *testing.T) {
	var s *scope.Scope
	if !s.IsInScope("anything.example.com") {
		t.Error("nil scope should allow all targets")
	}
	if !s.IsInScope("192.168.1.1") {
		t.Error("nil scope should allow IPs")
	}
}

func TestFilterTargets_NilScopePassesAll(t *testing.T) {
	var s *scope.Scope
	targets := []proute.ScanTarget{{Host: "api.example.com"}, {Host: "other.com"}}
	got, skipped := s.FilterTargets(targets)
	if len(got) != 2 || skipped != 0 {
		t.Errorf("nil scope should pass all: got %d in, %d skipped", len(got), skipped)
	}
}

func TestIsOutOfScope(t *testing.T) {
	s := build(t, []string{"*.example.com"}, nil)
	if !s.IsOutOfScope("other.com") {
		t.Error("expected other.com to be out of scope")
	}
	if s.IsOutOfScope("api.example.com") {
		t.Error("expected api.example.com to be in scope")
	}
}

func TestWildcardSingleLevel(t *testing.T) {
	s := build(t, []string{"*.example.com"}, nil)
	tests := []struct {
		target string
		want   bool
	}{
		{"api.example.com", true},
		{"staging.example.com", true},
		{"foo.bar.example.com", false}, // two levels — not matched by *.
		{"example.com", false},          // no subdomain
		{"notexample.com", false},
	}
	for _, tt := range tests {
		if got := s.IsInScope(tt.target); got != tt.want {
			t.Errorf("IsInScope(%q) = %v; want %v", tt.target, got, tt.want)
		}
	}
}

func TestDeepWildcard(t *testing.T) {
	s := build(t, []string{"**.example.com"}, nil)
	tests := []struct {
		target string
		want   bool
	}{
		{"foo.example.com", true},
		{"foo.bar.example.com", true},
		{"deep.nested.foo.example.com", true},
		{"example.com", false},   // the domain itself is not matched
		{"other.com", false},
	}
	for _, tt := range tests {
		if got := s.IsInScope(tt.target); got != tt.want {
			t.Errorf("IsInScope(%q) = %v; want %v", tt.target, got, tt.want)
		}
	}
}

func TestBareDomain(t *testing.T) {
	s := build(t, []string{"example.com"}, nil)
	tests := []struct {
		target string
		want   bool
	}{
		{"example.com", true},
		{"api.example.com", true},
		{"staging.example.com", true},
		{"deep.api.example.com", true},
		{"other.com", false},
		{"notexample.com", false},
	}
	for _, tt := range tests {
		if got := s.IsInScope(tt.target); got != tt.want {
			t.Errorf("IsInScope(%q) = %v; want %v", tt.target, got, tt.want)
		}
	}
}

func TestExactMatch(t *testing.T) {
	s := build(t, []string{"api.example.com"}, nil)
	if !s.IsInScope("api.example.com") {
		t.Error("expected exact match to be in scope")
	}
	if s.IsInScope("staging.example.com") {
		t.Error("expected non-matching domain to be out of scope")
	}
	if s.IsInScope("example.com") {
		t.Error("expected parent domain to be out of scope with exact pattern")
	}
}

func TestExcludeWithWildcard(t *testing.T) {
	// The exclude pattern "*.internal.example.com" is passed directly (no leading !).
	s := build(t,
		[]string{"*.example.com"},
		[]string{"*.internal.example.com"},
	)
	if s.IsInScope("db.internal.example.com") {
		t.Error("expected db.internal.example.com to be out of scope")
	}
	if !s.IsInScope("api.example.com") {
		t.Error("expected api.example.com to remain in scope")
	}
}

func TestLoadScopeFile(t *testing.T) {
	content := `# KiteString scope file
*.example.com
api.example.com
!staging.example.com
!*.internal.example.com
192.168.1.0/24
`
	f, err := os.CreateTemp(t.TempDir(), "scope*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()

	s, err := scope.LoadScope(f.Name())
	if err != nil {
		t.Fatalf("LoadScope: %v", err)
	}

	tests := []struct {
		target string
		want   bool
	}{
		{"api.example.com", true},
		{"www.example.com", true},
		{"staging.example.com", false},    // explicitly excluded
		{"db.internal.example.com", false}, // excluded via *.internal.example.com
		{"192.168.1.50", true},
		{"192.168.2.1", false},
		{"other.com", false},
	}
	for _, tt := range tests {
		if got := s.IsInScope(tt.target); got != tt.want {
			t.Errorf("IsInScope(%q) = %v; want %v", tt.target, got, tt.want)
		}
	}
}

func TestLoadScopeFile_NotFound(t *testing.T) {
	_, err := scope.LoadScope("/nonexistent/path/scope.txt")
	if err == nil {
		t.Error("expected error loading nonexistent file")
	}
}

func TestNoIncludePatternsAllowsAll(t *testing.T) {
	// Scope with only excludes: everything in scope except the excluded host.
	s := build(t, nil, []string{"bad.example.com"})
	if !s.IsInScope("api.example.com") {
		t.Error("expected api.example.com in scope when no includes defined")
	}
	if s.IsInScope("bad.example.com") {
		t.Error("expected bad.example.com out of scope (excluded)")
	}
}

func TestCaseInsensitiveMatching(t *testing.T) {
	s := build(t, []string{"*.Example.COM"}, nil)
	if !s.IsInScope("API.example.com") {
		t.Error("expected case-insensitive match")
	}
}
