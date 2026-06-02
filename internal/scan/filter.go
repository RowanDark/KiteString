package scan

import (
	"crypto/sha256"

	kshttp "github.com/RowanDark/kitestring/internal/http"
	"github.com/RowanDark/kitestring/pkg/proute"
)

// Filter returns true if result should be reported.
// FailStatusCodes always exclude matching codes.
// SuccessStatusCodes act as an allowlist when non-empty.
// IgnoreLengths suppress responses whose content length falls within any range.
func Filter(result *kshttp.Result, config proute.ScanConfig) bool {
	resp := result.Resp
	code := resp.StatusCode

	for _, fc := range config.FailStatusCodes {
		if code == fc {
			return false
		}
	}

	if len(config.SuccessStatusCodes) > 0 {
		allowed := false
		for _, sc := range config.SuccessStatusCodes {
			if code == sc {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}

	cl := int(resp.ContentLength)
	for _, lr := range config.IgnoreLengths {
		if lr.Contains(cl) {
			return false
		}
	}

	return true
}

// isWildcardNormalized reports whether a normalized response matches a preflight
// baseline, indicating the server returns an identical response for arbitrary paths.
func isWildcardNormalized(resp *kshttp.Response, baseline *Baseline) bool {
	if resp.StatusCode != baseline.StatusCode {
		return false
	}
	if resp.ContentLength != baseline.ContentLength {
		return false
	}
	if mimeType(resp.Headers.Get("Content-Type")) != mimeType(baseline.ContentType) {
		return false
	}
	return sha256.Sum256(resp.Body) == baseline.BodyHash
}
