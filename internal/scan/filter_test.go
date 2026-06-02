package scan_test

import (
	"bytes"
	"testing"

	kshttp "github.com/RowanDark/kitestring/internal/http"
	"github.com/RowanDark/kitestring/internal/scan"
	"github.com/RowanDark/kitestring/pkg/proute"
)

// TestSimilarityScore_IdenticalBodies verifies that two identical bodies score 1.0.
func TestSimilarityScore_IdenticalBodies(t *testing.T) {
	body := "The page you requested could not be found. Please check the URL and try again."
	score := scan.SimilarityScore(body, body)
	if score != 1.0 {
		t.Errorf("SimilarityScore(identical) = %.4f, want 1.0", score)
	}
}

// TestSimilarityScore_CompletelyDifferentBodies verifies that two completely
// different bodies score below 0.3.
func TestSimilarityScore_CompletelyDifferentBodies(t *testing.T) {
	a := "{"
	b := "zebra unicorn quantum flux capacitor banana helicopter submarine"
	score := scan.SimilarityScore(a, b)
	if score >= 0.3 {
		t.Errorf("SimilarityScore(different) = %.4f, want < 0.3", score)
	}
}

// TestSimilarityScore_EmptyBodies verifies that two empty bodies score 1.0.
func TestSimilarityScore_EmptyBodies(t *testing.T) {
	score := scan.SimilarityScore("", "")
	if score != 1.0 {
		t.Errorf("SimilarityScore(empty, empty) = %.4f, want 1.0", score)
	}
}

// TestIsTemplatedResponse_SuppressesMatchingBody verifies that a body identical
// to a baseline (simulating a templated 200 "page not found") is suppressed.
func TestIsTemplatedResponse_SuppressesMatchingBody(t *testing.T) {
	notFound := "404 - Page Not Found. The requested resource does not exist on this server."
	// Same content returned for a real scan path
	routeBody := notFound

	if !scan.IsTemplatedResponse(routeBody, []string{notFound}, 0.85) {
		t.Error("expected templated response to be detected, but IsTemplatedResponse returned false")
	}
}

// TestIsTemplatedResponse_PassesDifferentBody verifies that a legitimately
// different body is not suppressed.
func TestIsTemplatedResponse_PassesDifferentBody(t *testing.T) {
	notFound := "404 - Page Not Found. The requested resource does not exist on this server."
	apiResponse := `{"users":[{"id":1,"name":"alice"},{"id":2,"name":"bob"}],"total":2}`

	if scan.IsTemplatedResponse(apiResponse, []string{notFound}, 0.85) {
		t.Error("expected real API response to pass similarity filter, but IsTemplatedResponse returned true")
	}
}

// newMockResult builds a minimal kshttp.Result for filter testing.
func newMockResult(statusCode int, body []byte) *kshttp.Result {
	return &kshttp.Result{
		Req: &kshttp.Request{
			FullURL: "http://example.com/test",
		},
		Resp: &kshttp.Response{
			StatusCode:    statusCode,
			ContentLength: int64(len(body)),
			Body:          body,
		},
	}
}

// TestFilter_TemplatedResponseSuppressed verifies that a 200 response whose body
// matches a baseline "page not found" body is filtered with a similarity reason.
func TestFilter_TemplatedResponseSuppressed(t *testing.T) {
	notFoundBody := "404 - Page Not Found. The requested resource does not exist on this server."
	result := newMockResult(200, []byte(notFoundBody))
	config := proute.ScanConfig{
		SimilarityThreshold: 0.85,
	}

	fr := scan.Filter(result, config, []string{notFoundBody})

	if fr.Passed {
		t.Error("expected templated 200 to be filtered, but Filter returned Passed=true")
	}
	if fr.Score < 0.85 {
		t.Errorf("expected Score >= 0.85, got %.4f", fr.Score)
	}
	if fr.Reason == "" {
		t.Error("expected non-empty Reason for filtered result")
	}
}

// TestFilter_DisableSimilarityPassesThrough verifies that --disable-similarity
// bypasses similarity scoring entirely and passes the result through.
func TestFilter_DisableSimilarityPassesThrough(t *testing.T) {
	notFoundBody := "404 - Page Not Found. The requested resource does not exist on this server."
	result := newMockResult(200, []byte(notFoundBody))
	config := proute.ScanConfig{
		DisableSimilarity:   true,
		SimilarityThreshold: 0.85,
	}

	fr := scan.Filter(result, config, []string{notFoundBody})

	if !fr.Passed {
		t.Errorf("expected DisableSimilarity to pass result through, but got Reason=%q", fr.Reason)
	}
}

// TestFilter_ContentLengthRange verifies that a response within a configured
// ignore-length range is suppressed with a length reason.
func TestFilter_ContentLengthRange(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 150)
	result := newMockResult(200, body)

	lr, err := proute.ParseLengthRange("100-200")
	if err != nil {
		t.Fatalf("ParseLengthRange: %v", err)
	}
	config := proute.ScanConfig{
		IgnoreLengths: []proute.LengthRange{lr},
	}

	fr := scan.Filter(result, config, nil)

	if fr.Passed {
		t.Error("expected 150-byte response in range 100-200 to be filtered, but got Passed=true")
	}
	if fr.Reason == "" {
		t.Error("expected non-empty Reason for length-filtered result")
	}
}

// TestFilter_ContentLengthBelowRange verifies that a response outside the
// ignore-length range is not suppressed.
func TestFilter_ContentLengthBelowRange(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 50)
	result := newMockResult(200, body)

	lr, err := proute.ParseLengthRange("100-200")
	if err != nil {
		t.Fatalf("ParseLengthRange: %v", err)
	}
	config := proute.ScanConfig{
		IgnoreLengths: []proute.LengthRange{lr},
	}

	fr := scan.Filter(result, config, nil)

	if !fr.Passed {
		t.Errorf("expected 50-byte response (outside 100-200) to pass, but got Reason=%q", fr.Reason)
	}
}

// TestFilter_FailStatusCode verifies that a blacklisted status code is suppressed.
func TestFilter_FailStatusCode(t *testing.T) {
	result := newMockResult(404, []byte("not found"))
	config := proute.ScanConfig{
		FailStatusCodes: []int{404},
	}

	fr := scan.Filter(result, config, nil)

	if fr.Passed {
		t.Error("expected 404 to be filtered by FailStatusCodes, but got Passed=true")
	}
}

// TestFilter_SuccessStatusAllowlist verifies that an unlisted status code is
// suppressed when SuccessStatusCodes is non-empty.
func TestFilter_SuccessStatusAllowlist(t *testing.T) {
	result := newMockResult(403, []byte("forbidden"))
	config := proute.ScanConfig{
		SuccessStatusCodes: []int{200, 201},
	}

	fr := scan.Filter(result, config, nil)

	if fr.Passed {
		t.Error("expected 403 to be filtered by SuccessStatusCodes allowlist, but got Passed=true")
	}
}

// TestFilter_NoBaselinesSkipsSimilarity verifies that when no baselines are
// provided, similarity scoring is skipped and the result passes.
func TestFilter_NoBaselinesSkipsSimilarity(t *testing.T) {
	notFoundBody := "404 - Page Not Found. The requested resource does not exist."
	result := newMockResult(200, []byte(notFoundBody))
	config := proute.ScanConfig{
		SimilarityThreshold: 0.85,
	}

	fr := scan.Filter(result, config, nil) // no baselines

	if !fr.Passed {
		t.Errorf("expected result to pass when no baselines provided, but got Reason=%q", fr.Reason)
	}
}
