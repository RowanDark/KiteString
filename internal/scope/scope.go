package scope

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/RowanDark/kitestring/pkg/proute"
)

// Scope holds include and exclude pattern lists for target filtering.
type Scope struct {
	includes []string
	excludes []string
}

// New creates a Scope from explicit include and exclude pattern lists.
func New(includes, excludes []string) *Scope {
	return &Scope{includes: includes, excludes: excludes}
}

// LoadScope parses a scope file and returns a Scope.
// Lines starting with # are comments; lines starting with ! are exclude patterns.
func LoadScope(path string) (*Scope, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open scope file: %w", err)
	}
	defer f.Close()

	s := &Scope{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "!") {
			s.excludes = append(s.excludes, line[1:])
		} else {
			s.includes = append(s.includes, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading scope file: %w", err)
	}
	return s, nil
}

// Includes returns the raw include patterns.
func (s *Scope) Includes() []string { return s.includes }

// Excludes returns the raw exclude patterns.
func (s *Scope) Excludes() []string { return s.excludes }

// IsInScope returns true if target matches an include pattern and no exclude pattern.
// If no include patterns are defined, all targets not matching an exclude are in scope.
func (s *Scope) IsInScope(target string) bool {
	if s == nil || (len(s.includes) == 0 && len(s.excludes) == 0) {
		return true
	}

	host := extractHost(target)

	for _, pattern := range s.excludes {
		if matchPattern(pattern, host) {
			return false
		}
	}

	if len(s.includes) == 0 {
		return true
	}

	for _, pattern := range s.includes {
		if matchPattern(pattern, host) {
			return true
		}
	}
	return false
}

// IsOutOfScope is the inverse of IsInScope.
func (s *Scope) IsOutOfScope(target string) bool {
	return !s.IsInScope(target)
}

// FilterTargets returns only in-scope targets and a count of skipped ones.
func (s *Scope) FilterTargets(targets []proute.ScanTarget) ([]proute.ScanTarget, int) {
	if s == nil {
		return targets, 0
	}
	var inScope []proute.ScanTarget
	for _, t := range targets {
		if s.IsInScope(t.Host) {
			inScope = append(inScope, t)
		}
	}
	return inScope, len(targets) - len(inScope)
}

// extractHost pulls the hostname from a URL string, stripping port if present.
func extractHost(target string) string {
	if strings.Contains(target, "://") {
		u, err := url.Parse(target)
		if err == nil {
			return u.Hostname()
		}
	}
	host, _, err := net.SplitHostPort(target)
	if err == nil {
		return host
	}
	return target
}

// matchPattern reports whether host matches a scope pattern.
//
// Pattern forms:
//   - *.example.com    — exactly one subdomain level
//   - **.example.com   — any depth of subdomains
//   - 192.168.1.0/24   — CIDR range matched against IP addresses
//   - example.com      — exact match plus all subdomains (implied wildcard)
func matchPattern(pattern, host string) bool {
	// CIDR range
	if strings.Contains(pattern, "/") {
		_, ipNet, err := net.ParseCIDR(pattern)
		if err == nil {
			ip := net.ParseIP(host)
			return ip != nil && ipNet.Contains(ip)
		}
	}

	// Deep wildcard: **.example.com matches example.com and any subdomain depth
	if strings.HasPrefix(pattern, "**.") {
		base := pattern[3:]
		return host == base || strings.HasSuffix(host, "."+base)
	}

	// Single-level wildcard: *.example.com matches sub.example.com but not a.b.example.com
	if strings.HasPrefix(pattern, "*.") {
		base := pattern[2:]
		if !strings.HasSuffix(host, "."+base) {
			return false
		}
		prefix := host[:len(host)-len(base)-1]
		return !strings.Contains(prefix, ".")
	}

	// Bare domain with implied wildcard: example.com matches example.com and all subdomains
	return host == pattern || strings.HasSuffix(host, "."+pattern)
}
