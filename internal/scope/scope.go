package scope

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/RowanDark/kitestring/pkg/proute"
)

type patternKind int

const (
	kindExact      patternKind = iota
	kindWildcard               // *.example.com — exactly one subdomain level
	kindDeepWild               // **.example.com — any depth of subdomains
	kindBareDomain             // example.com — matches itself and all subdomains
	kindCIDR                   // 192.168.1.0/24
)

type pattern struct {
	kind  patternKind
	value string     // normalised lowercase value for domain patterns
	cidr  *net.IPNet // non-nil for kindCIDR
}

// Scope holds include and exclude patterns for target filtering.
type Scope struct {
	includes []pattern
	excludes []pattern
}

// New returns an empty Scope ready for AddInclude/AddExclude calls.
func New() *Scope {
	return &Scope{}
}

// LoadScope parses the file at path and returns a populated Scope.
// File format:
//
//	# comment
//	*.example.com              → include (wildcard one level)
//	**.example.com             → include (wildcard any depth)
//	api.example.com            → include (exact)
//	example.com                → include (bare domain — implies all subdomains)
//	!staging.example.com       → exclude
//	192.168.1.0/24             → include (CIDR)
func LoadScope(path string) (*Scope, error) {
	s := New()
	if err := s.LoadFile(path); err != nil {
		return nil, err
	}
	return s, nil
}

// LoadFile parses a scope file and appends its patterns to s.
func (s *Scope) LoadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open scope file %q: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "!") {
			if err := s.AddExclude(line[1:]); err != nil {
				return fmt.Errorf("scope file line %d: %w", lineNum, err)
			}
		} else {
			if err := s.AddInclude(line); err != nil {
				return fmt.Errorf("scope file line %d: %w", lineNum, err)
			}
		}
	}
	return scanner.Err()
}

// AddInclude parses raw and appends it to the include list.
func (s *Scope) AddInclude(raw string) error {
	p, err := parsePattern(raw)
	if err != nil {
		return err
	}
	s.includes = append(s.includes, p)
	return nil
}

// AddExclude parses raw and appends it to the exclude list.
func (s *Scope) AddExclude(raw string) error {
	p, err := parsePattern(raw)
	if err != nil {
		return err
	}
	s.excludes = append(s.excludes, p)
	return nil
}

// Len returns the total number of patterns (includes + excludes).
func (s *Scope) Len() int {
	if s == nil {
		return 0
	}
	return len(s.includes) + len(s.excludes)
}

// IsInScope reports whether target is within scope.
//
// A target is in scope when:
//   - it matches at least one include pattern (or there are no include patterns), AND
//   - it does not match any exclude pattern.
//
// A nil Scope always returns true (no filtering).
func (s *Scope) IsInScope(target string) bool {
	if s == nil {
		return true
	}
	if len(s.includes) > 0 {
		matched := false
		for _, p := range s.includes {
			if p.matches(target) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for _, p := range s.excludes {
		if p.matches(target) {
			return false
		}
	}
	return true
}

// IsOutOfScope is the inverse of IsInScope.
func (s *Scope) IsOutOfScope(target string) bool {
	return !s.IsInScope(target)
}

// FilterTargets returns only the in-scope targets and the count of skipped ones.
// A nil Scope returns all targets with skipped=0.
func (s *Scope) FilterTargets(targets []proute.ScanTarget) (inScope []proute.ScanTarget, skipped int) {
	if s == nil {
		return targets, 0
	}
	inScope = make([]proute.ScanTarget, 0, len(targets))
	for _, t := range targets {
		if s.IsInScope(t.Host) {
			inScope = append(inScope, t)
		} else {
			skipped++
		}
	}
	return inScope, skipped
}

// parsePattern classifies and normalises a raw pattern string.
func parsePattern(raw string) (pattern, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return pattern{}, fmt.Errorf("empty pattern")
	}

	// CIDR: only when it contains '/' and is not a wildcard pattern.
	if strings.Contains(raw, "/") && !strings.HasPrefix(raw, "*") {
		_, ipNet, err := net.ParseCIDR(raw)
		if err != nil {
			return pattern{}, fmt.Errorf("invalid CIDR %q: %w", raw, err)
		}
		return pattern{kind: kindCIDR, cidr: ipNet}, nil
	}

	// Deep wildcard: **.example.com
	if strings.HasPrefix(raw, "**.") {
		return pattern{
			kind:  kindDeepWild,
			value: strings.ToLower(strings.TrimPrefix(raw, "**.")),
		}, nil
	}

	// Single-level wildcard: *.example.com
	if strings.HasPrefix(raw, "*.") {
		return pattern{
			kind:  kindWildcard,
			value: strings.ToLower(strings.TrimPrefix(raw, "*.")),
		}, nil
	}

	normalized := strings.ToLower(raw)
	labels := strings.Split(normalized, ".")

	// Bare domain (exactly 2 non-empty labels, e.g. "example.com") implies all subdomains.
	if len(labels) == 2 && labels[0] != "" && labels[1] != "" {
		return pattern{kind: kindBareDomain, value: normalized}, nil
	}

	// Everything else is an exact match.
	return pattern{kind: kindExact, value: normalized}, nil
}

// matches reports whether target is covered by this pattern.
func (p pattern) matches(target string) bool {
	tl := strings.ToLower(strings.TrimSpace(target))
	switch p.kind {
	case kindExact:
		return tl == p.value

	case kindWildcard:
		// *.example.com: exactly one label before .example.com
		suffix := "." + p.value
		if !strings.HasSuffix(tl, suffix) {
			return false
		}
		prefix := tl[:len(tl)-len(suffix)]
		return prefix != "" && !strings.Contains(prefix, ".")

	case kindDeepWild:
		// **.example.com: one or more labels before .example.com
		suffix := "." + p.value
		if !strings.HasSuffix(tl, suffix) {
			return false
		}
		return len(tl) > len(suffix)

	case kindBareDomain:
		// example.com matches itself and any subdomain depth.
		return tl == p.value || strings.HasSuffix(tl, "."+p.value)

	case kindCIDR:
		ip := net.ParseIP(tl)
		if ip == nil {
			return false
		}
		return p.cidr.Contains(ip)
	}
	return false
}
