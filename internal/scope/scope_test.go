package scope

import (
	"os"
	"testing"

	"github.com/RowanDark/kitestring/pkg/proute"
)

func TestIsInScope_WildcardInclude(t *testing.T) {
	s := New([]string{"*.example.com"}, nil)
	if !s.IsInScope("api.example.com") {
		t.Error("api.example.com should be in scope with *.example.com include")
	}
}

func TestIsInScope_WildcardInclude_NoMatch(t *testing.T) {
	s := New([]string{"*.example.com"}, nil)
	if s.IsInScope("api.other.com") {
		t.Error("api.other.com should not be in scope with *.example.com include")
	}
}

func TestIsInScope_WildcardInclude_DeepSubdomain(t *testing.T) {
	s := New([]string{"*.example.com"}, nil)
	// *.example.com should NOT match a.b.example.com (two levels deep)
	if s.IsInScope("a.b.example.com") {
		t.Error("a.b.example.com should not match *.example.com (only one subdomain level)")
	}
}

func TestIsInScope_ExcludeOverridesInclude(t *testing.T) {
	s := New([]string{"*.example.com"}, []string{"staging.example.com"})
	if s.IsInScope("staging.example.com") {
		t.Error("staging.example.com should be out of scope due to !staging.example.com exclude")
	}
}

func TestIsInScope_NoMatchingInclude(t *testing.T) {
	s := New([]string{"*.example.com"}, nil)
	if s.IsInScope("api.other.com") {
		t.Error("api.other.com should not be in scope with no matching include")
	}
}

func TestIsInScope_DeepWildcard(t *testing.T) {
	s := New([]string{"**.example.com"}, nil)
	if !s.IsInScope("a.b.c.example.com") {
		t.Error("a.b.c.example.com should be in scope with **.example.com")
	}
	if !s.IsInScope("example.com") {
		t.Error("example.com should be in scope with **.example.com")
	}
}

func TestIsInScope_BareDomainImpliedWildcard(t *testing.T) {
	s := New([]string{"example.com"}, nil)
	if !s.IsInScope("example.com") {
		t.Error("example.com should be in scope")
	}
	if !s.IsInScope("api.example.com") {
		t.Error("api.example.com should be in scope with bare example.com include")
	}
	if !s.IsInScope("a.b.example.com") {
		t.Error("a.b.example.com should be in scope with bare example.com include")
	}
}

func TestIsInScope_ExactExclude(t *testing.T) {
	s := New(nil, []string{"staging.example.com"})
	if s.IsInScope("staging.example.com") {
		t.Error("staging.example.com should be excluded")
	}
	if !s.IsInScope("api.example.com") {
		t.Error("api.example.com should be in scope (no includes defined, only exclude)")
	}
}

func TestIsInScope_NoPatterns(t *testing.T) {
	s := New(nil, nil)
	if !s.IsInScope("anything.com") {
		t.Error("anything should be in scope when no patterns defined")
	}
}

func TestIsInScope_NilScope(t *testing.T) {
	var s *Scope
	if !s.IsInScope("anything.com") {
		t.Error("nil scope should treat everything as in scope")
	}
}

func TestIsInScope_CIDR_Accept(t *testing.T) {
	s := New([]string{"192.168.1.0/24"}, nil)
	if !s.IsInScope("192.168.1.1") {
		t.Error("192.168.1.1 should be in scope for 192.168.1.0/24")
	}
	if !s.IsInScope("192.168.1.254") {
		t.Error("192.168.1.254 should be in scope for 192.168.1.0/24")
	}
}

func TestIsInScope_CIDR_Reject(t *testing.T) {
	s := New([]string{"192.168.1.0/24"}, nil)
	if s.IsInScope("192.168.2.1") {
		t.Error("192.168.2.1 should not be in scope for 192.168.1.0/24")
	}
	if s.IsInScope("10.0.0.1") {
		t.Error("10.0.0.1 should not be in scope for 192.168.1.0/24")
	}
}

func TestIsInScope_CIDR_Exclude(t *testing.T) {
	s := New([]string{"192.168.0.0/16"}, []string{"192.168.1.0/24"})
	if s.IsInScope("192.168.1.5") {
		t.Error("192.168.1.5 should be excluded by 192.168.1.0/24")
	}
	if !s.IsInScope("192.168.2.1") {
		t.Error("192.168.2.1 should be in scope (in /16 but not in excluded /24)")
	}
}

func TestIsOutOfScope(t *testing.T) {
	s := New([]string{"*.example.com"}, nil)
	if !s.IsOutOfScope("other.com") {
		t.Error("other.com should be out of scope")
	}
	if s.IsOutOfScope("api.example.com") {
		t.Error("api.example.com should not be out of scope")
	}
}

func TestFilterTargets(t *testing.T) {
	s := New([]string{"*.example.com"}, []string{"staging.example.com"})
	targets := []proute.ScanTarget{
		{Host: "api.example.com"},
		{Host: "staging.example.com"},
		{Host: "other.com"},
		{Host: "admin.example.com"},
	}
	inScope, skipped := s.FilterTargets(targets)
	if len(inScope) != 2 {
		t.Errorf("expected 2 in-scope targets, got %d", len(inScope))
	}
	if skipped != 2 {
		t.Errorf("expected 2 skipped, got %d", skipped)
	}
	for _, target := range inScope {
		if target.Host != "api.example.com" && target.Host != "admin.example.com" {
			t.Errorf("unexpected in-scope target: %s", target.Host)
		}
	}
}

func TestFilterTargets_NilScope(t *testing.T) {
	var s *Scope
	targets := []proute.ScanTarget{{Host: "any.com"}}
	out, skipped := s.FilterTargets(targets)
	if len(out) != 1 || skipped != 0 {
		t.Error("nil scope should pass all targets through")
	}
}

func TestLoadScope(t *testing.T) {
	content := `# KiteString scope file
*.example.com
api.example.com
!staging.example.com
!*.internal.example.com
192.168.1.0/24
`
	f, err := os.CreateTemp("", "scope-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(content)
	f.Close()

	s, err := LoadScope(f.Name())
	if err != nil {
		t.Fatalf("LoadScope: %v", err)
	}
	if len(s.includes) != 3 {
		t.Errorf("expected 3 includes, got %d", len(s.includes))
	}
	if len(s.excludes) != 2 {
		t.Errorf("expected 2 excludes, got %d", len(s.excludes))
	}

	if !s.IsInScope("api.example.com") {
		t.Error("api.example.com should be in scope")
	}
	if s.IsInScope("staging.example.com") {
		t.Error("staging.example.com should be excluded")
	}
	if s.IsInScope("foo.internal.example.com") {
		t.Error("foo.internal.example.com should be excluded by *.internal.example.com")
	}
	if !s.IsInScope("192.168.1.10") {
		t.Error("192.168.1.10 should be in CIDR range")
	}
}

func TestLoadScope_NotFound(t *testing.T) {
	_, err := LoadScope("/nonexistent/scope.txt")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestIsInScope_URLTarget(t *testing.T) {
	s := New([]string{"*.example.com"}, nil)
	if !s.IsInScope("https://api.example.com") {
		t.Error("URL with in-scope host should be in scope")
	}
	if s.IsInScope("https://api.other.com") {
		t.Error("URL with out-of-scope host should be out of scope")
	}
}
