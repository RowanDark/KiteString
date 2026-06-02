package scan

import (
	"crypto/sha256"
	"fmt"
	"hash/fnv"
	"strings"

	kshttp "github.com/RowanDark/kitestring/internal/http"
	"github.com/RowanDark/kitestring/pkg/proute"
)

// DefaultSimilarityThreshold is the default body similarity threshold above
// which a response is considered a templated error page and suppressed.
const DefaultSimilarityThreshold = 0.85

// numMinHashFunctions is the number of hash functions used in the MinHash
// signature. More functions improve accuracy at the cost of computation time.
const numMinHashFunctions = 64

// FilterResult records the outcome of a filter decision with a human-readable
// reason and, when similarity scoring was performed, the computed score.
type FilterResult struct {
	Passed bool
	Reason string
	Score  float64
}

// SimilarityScore returns a 0.0–1.0 Jaccard similarity estimate between two
// response bodies using a MinHash signature over lowercased word tokens.
// Completely disjoint bodies always score 0.0; identical bodies score 1.0.
func SimilarityScore(a, b string) float64 {
	tokA := strings.Fields(strings.ToLower(a))
	tokB := strings.Fields(strings.ToLower(b))

	if len(tokA) == 0 && len(tokB) == 0 {
		return 1.0
	}
	if len(tokA) == 0 || len(tokB) == 0 {
		return 0.0
	}

	sigA := minHashSig(tokA)
	sigB := minHashSig(tokB)

	matches := 0
	for i := range sigA {
		if sigA[i] == sigB[i] {
			matches++
		}
	}
	return float64(matches) / float64(numMinHashFunctions)
}

// IsTemplatedResponse reports whether body is similar to any of the provided
// baseline bodies beyond the given threshold, indicating a templated error page
// that should be suppressed even when it returns a 200 OK status code.
func IsTemplatedResponse(body string, baselines []string, threshold float64) bool {
	for _, bl := range baselines {
		if SimilarityScore(body, bl) >= threshold {
			return true
		}
	}
	return false
}

// Filter evaluates result against all configured filters and returns a
// FilterResult describing whether the result should be reported and why not.
// baselines contains baseline response bodies collected during preflight for
// similarity comparison; pass nil to skip similarity scoring.
func Filter(result *kshttp.Result, config proute.ScanConfig, baselines []string) FilterResult {
	resp := result.Resp
	code := resp.StatusCode

	for _, fc := range config.FailStatusCodes {
		if code == fc {
			return FilterResult{Reason: fmt.Sprintf("status %d", code)}
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
			return FilterResult{Reason: fmt.Sprintf("status %d not in allowlist", code)}
		}
	}

	cl := int(resp.ContentLength)
	for _, lr := range config.IgnoreLengths {
		if lr.Contains(cl) {
			return FilterResult{Reason: fmt.Sprintf("length %d ignored (range %d-%d)", cl, lr.Min, lr.Max)}
		}
	}

	if !config.DisableSimilarity && len(baselines) > 0 {
		threshold := config.SimilarityThreshold
		if threshold <= 0 {
			threshold = DefaultSimilarityThreshold
		}
		body := string(resp.Body)
		for _, bl := range baselines {
			score := SimilarityScore(body, bl)
			if score >= threshold {
				return FilterResult{
					Reason: fmt.Sprintf("similarity %.2f exceeds threshold %.2f", score, threshold),
					Score:  score,
				}
			}
		}
	}

	return FilterResult{Passed: true}
}

// minHashSig computes a MinHash signature of numMinHashFunctions minimum hash
// values over the token set, estimating Jaccard similarity when compared
// against another signature. For completely disjoint token sets, all
// corresponding minima will differ, giving a similarity estimate of 0.0.
func minHashSig(tokens []string) [numMinHashFunctions]uint64 {
	var sig [numMinHashFunctions]uint64
	for i := range sig {
		sig[i] = ^uint64(0) // MaxUint64
	}
	for _, tok := range tokens {
		for i := 0; i < numMinHashFunctions; i++ {
			if h := seededHash(tok, uint64(i)); h < sig[i] {
				sig[i] = h
			}
		}
	}
	return sig
}

// seededHash returns a 64-bit FNV-1a hash of s mixed with a seed value so that
// each seed index produces an independent hash family member.
func seededHash(s string, seed uint64) uint64 {
	h := fnv.New64a()
	var buf [8]byte
	for j := 0; j < 8; j++ {
		buf[j] = byte(seed >> (uint(j) * 8))
	}
	h.Write(buf[:])  //nolint:errcheck
	h.Write([]byte(s)) //nolint:errcheck
	return h.Sum64()
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
