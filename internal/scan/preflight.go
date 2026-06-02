package scan

import (
	"crypto/sha256"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"

	"github.com/RowanDark/kitestring/pkg/proute"
)

// HostProfile records basic liveness and baseline behavior for a target.
type HostProfile struct {
	StatusCode    int
	ContentLength int64
	ContentType   string
}

// maxBaselineBodyText is the maximum number of bytes stored in Baseline.BodyText
// for similarity scoring. Bodies longer than this are truncated.
const maxBaselineBodyText = 64 * 1024

// Baseline captures the response signature of a path prefix used for wildcard detection.
type Baseline struct {
	PathPrefix    string
	StatusCode    int
	ContentLength int64
	ContentType   string
	BodyHash      [32]byte
	BodyText      string // truncated body for similarity scoring; see maxBaselineBodyText
}

// CheckHost sends a HEAD request to the target root, confirms reachability,
// and records the baseline status and content length.
func CheckHost(target proute.ScanTarget, client *http.Client) (*HostProfile, error) {
	url := buildTargetURL(target, "/")
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build head request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("host unreachable %s: %w", target.Host, err)
	}
	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	resp.Body.Close()

	cl := resp.ContentLength
	if cl < 0 {
		cl = 0
	}
	return &HostProfile{
		StatusCode:    resp.StatusCode,
		ContentLength: cl,
		ContentType:   resp.Header.Get("Content-Type"),
	}, nil
}

// BuildBaselines extracts unique path prefixes up to depth levels from routes,
// probes each prefix with a random-suffix request, and returns response signatures.
// depth 0 checks only the docroot; depth 1 checks one level of path prefixes.
func BuildBaselines(target proute.ScanTarget, routes []proute.Route, depth int, client *http.Client) (map[string]*Baseline, error) {
	prefixes := extractPrefixes(routes, depth)
	baselines := make(map[string]*Baseline, len(prefixes))

	for _, prefix := range prefixes {
		probe := strings.TrimSuffix(prefix, "/") + "/" + randomSuffix(16)
		url := buildTargetURL(target, probe)

		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		if err != nil {
			continue
		}

		cl := resp.ContentLength
		if cl < 0 {
			cl = int64(len(body))
		}

		bt := string(body)
		if len(bt) > maxBaselineBodyText {
			bt = bt[:maxBaselineBodyText]
		}

		baselines[prefix] = &Baseline{
			PathPrefix:    prefix,
			StatusCode:    resp.StatusCode,
			ContentLength: cl,
			ContentType:   resp.Header.Get("Content-Type"),
			BodyHash:      sha256.Sum256(body),
			BodyText:      bt,
		}
	}

	return baselines, nil
}

// IsWildcard reads the response body, compares the full response signature to the
// baseline, and returns true if they match (indicating wildcard routing).
// The response body is consumed and closed by this call.
func IsWildcard(resp *http.Response, baseline *Baseline) bool {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	resp.Body.Close()
	if err != nil {
		return false
	}

	if resp.StatusCode != baseline.StatusCode {
		return false
	}

	cl := resp.ContentLength
	if cl < 0 {
		cl = int64(len(body))
	}
	if cl != baseline.ContentLength {
		return false
	}

	if mimeType(resp.Header.Get("Content-Type")) != mimeType(baseline.ContentType) {
		return false
	}

	return sha256.Sum256(body) == baseline.BodyHash
}

// Preflight runs host liveness and baseline probing for a target.
// When disabled is true, all probing is skipped and nil results are returned.
func Preflight(target proute.ScanTarget, routes []proute.Route, depth int, disabled bool, client *http.Client) (*HostProfile, map[string]*Baseline, error) {
	if disabled {
		return nil, nil, nil
	}

	profile, err := CheckHost(target, client)
	if err != nil {
		return nil, nil, err
	}

	baselines, err := BuildBaselines(target, routes, depth, client)
	if err != nil {
		return nil, nil, err
	}

	return profile, baselines, nil
}

// extractPrefixes returns deduplicated path prefixes at the configured depth.
// When routes is empty or depth is 0, only the docroot ("/") is returned.
func extractPrefixes(routes []proute.Route, depth int) []string {
	seen := make(map[string]struct{})
	for _, r := range routes {
		seen[prefixAtDepth(r.Path, depth)] = struct{}{}
	}
	if len(seen) == 0 {
		return []string{"/"}
	}
	prefixes := make([]string, 0, len(seen))
	for p := range seen {
		prefixes = append(prefixes, p)
	}
	return prefixes
}

// prefixAtDepth returns the first depth segments of path as a prefix.
// depth 0 always returns "/".
func prefixAtDepth(path string, depth int) string {
	if depth == 0 {
		return "/"
	}
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return "/"
	}
	parts := strings.SplitN(path, "/", depth+1)
	if len(parts) <= depth {
		return "/" + strings.Join(parts, "/")
	}
	return "/" + strings.Join(parts[:depth], "/")
}

// buildTargetURL assembles a full URL for the target with the given path.
func buildTargetURL(t proute.ScanTarget, path string) string {
	var base string
	if (t.Scheme == "http" && t.Port == 80) ||
		(t.Scheme == "https" && t.Port == 443) ||
		t.Port == 0 {
		base = fmt.Sprintf("%s://%s", t.Scheme, t.Host)
	} else {
		base = fmt.Sprintf("%s://%s:%d", t.Scheme, t.Host, t.Port)
	}
	basePath := strings.TrimSuffix(t.BasePath, "/")
	return base + basePath + path
}

const randChars = "abcdefghijklmnopqrstuvwxyz0123456789"

func randomSuffix(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = randChars[rand.Intn(len(randChars))]
	}
	return string(b)
}

func mimeType(ct string) string {
	if idx := strings.IndexByte(ct, ';'); idx >= 0 {
		return strings.TrimSpace(ct[:idx])
	}
	return strings.TrimSpace(ct)
}
