package replay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	neturl "net/url"
	"regexp"
	"strings"
	"time"

	kswordlist "github.com/RowanDark/kitestring/internal/wordlist"
	"github.com/RowanDark/kitestring/pkg/proute"
)

// ReplayRequest holds everything needed to reconstruct and execute an HTTP request.
type ReplayRequest struct {
	Method        string
	URL           string
	Headers       map[string]string
	Body          []byte
	KSUID         string
	WordlistPaths []string
}

// ReplayResponse holds the result of a replayed request.
type ReplayResponse struct {
	StatusCode int
	Headers    nethttp.Header
	Body       []byte
	Duration   time.Duration
	RawRequest string
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// ParseResultLine parses a single result line from any KiteString output format
// (pretty, text, or JSONL) into a ReplayRequest.
func ParseResultLine(line string) (*ReplayRequest, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, fmt.Errorf("empty input line")
	}

	// JSONL: starts with '{'
	if strings.HasPrefix(line, "{") {
		return parseJSONL(line)
	}

	// Strip ANSI escape sequences (pretty format uses color codes)
	clean := ansiRe.ReplaceAllString(line, "")
	fields := strings.Fields(clean)
	if len(fields) < 5 {
		return nil, fmt.Errorf("too few fields (%d) in result line: %q", len(fields), line)
	}

	// Distinguish pretty from text by checking whether the first field is a 3-digit status code.
	// Pretty format: STATUS  METHOD  LENGTH  TIME  URL  [KSUID]
	// Text format:   METHOD  STATUS  LENGTH  TIME  URL  [KSUID]
	if isPureDigits(fields[0]) && len(fields[0]) == 3 {
		return parsePrettyFields(fields)
	}
	return parseTextFields(fields)
}

func isPureDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

type jsonResultLine struct {
	Method         string `json:"method"`
	URL            string `json:"url"`
	StatusCode     int    `json:"status_code"`
	ContentLength  int    `json:"content_length"`
	ResponseTimeMs int64  `json:"response_time_ms"`
	Timestamp      string `json:"timestamp"`
	KSUID          string `json:"ksuid"`
}

func parseJSONL(line string) (*ReplayRequest, error) {
	var jl jsonResultLine
	if err := json.Unmarshal([]byte(line), &jl); err != nil {
		return nil, fmt.Errorf("parse JSONL: %w", err)
	}
	if jl.URL == "" {
		return nil, fmt.Errorf("JSONL result line missing 'url' field")
	}
	method := strings.ToUpper(strings.TrimSpace(jl.Method))
	if method == "" {
		method = "GET"
	}
	return &ReplayRequest{
		Method: method,
		URL:    jl.URL,
		KSUID:  jl.KSUID,
	}, nil
}

// parsePrettyFields parses the pretty output format after ANSI stripping:
//
//	STATUS  METHOD  LENGTH  TIME  URL  [KSUID]
func parsePrettyFields(fields []string) (*ReplayRequest, error) {
	method := strings.ToUpper(fields[1])
	url := fields[4]
	ksuid := ""
	if len(fields) > 5 {
		ksuid = fields[5]
	}
	return &ReplayRequest{Method: method, URL: url, KSUID: ksuid}, nil
}

// parseTextFields parses the text output format:
//
//	METHOD  STATUS  LENGTH  TIME  URL  [KSUID]
func parseTextFields(fields []string) (*ReplayRequest, error) {
	method := strings.ToUpper(fields[0])
	url := fields[4]
	ksuid := ""
	if len(fields) > 5 {
		ksuid = fields[5]
	}
	return &ReplayRequest{Method: method, URL: url, KSUID: ksuid}, nil
}

// Reconstruct looks up the KSUID in wordlistPaths (if provided) to find the original
// route definition, then rebuilds the HTTP request with generated parameter values.
// Falls back to a plain URL+method request when no matching route is found.
func (r *ReplayRequest) Reconstruct(wordlistPaths []string) (*nethttp.Request, error) {
	if r.KSUID != "" && len(wordlistPaths) > 0 {
		routes, err := kswordlist.Load(wordlistPaths)
		if err == nil {
			for _, route := range routes {
				if route.KSUID == r.KSUID {
					return r.buildFromRoute(route)
				}
			}
		}
	}
	return r.buildFallback()
}

// buildFromRoute rebuilds a request using the route's Crumb-defined parameters,
// replacing any existing query params in the URL with freshly generated values.
func (r *ReplayRequest) buildFromRoute(route proute.Route) (*nethttp.Request, error) {
	parsed, err := neturl.Parse(r.URL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	// Rebuild query params from route definition, discarding the scan-time values.
	if len(route.QueryParams) > 0 {
		vals := neturl.Values{}
		for _, c := range route.QueryParams {
			vals.Set(c.Key, c.GenerateValue())
		}
		parsed.RawQuery = vals.Encode()
	} else {
		parsed.RawQuery = ""
	}

	// Merge: route headers on top of any pre-set headers on the ReplayRequest.
	headers := make(map[string]string, len(r.Headers)+len(route.Headers))
	for k, v := range r.Headers {
		headers[k] = v
	}
	for _, c := range route.Headers {
		headers[c.Key] = c.GenerateValue()
	}

	method := strings.ToUpper(route.Method)
	if method == "" {
		method = r.Method
	}

	var body []byte
	if len(route.BodyParams) > 0 && hasBody(method) {
		ct := route.ContentType
		if strings.Contains(ct, "application/json") {
			m := make(map[string]interface{}, len(route.BodyParams))
			for _, c := range route.BodyParams {
				m[c.Key] = c.GenerateValue()
			}
			body, err = json.Marshal(m)
			if err != nil {
				return nil, fmt.Errorf("marshal body: %w", err)
			}
			if headers["Content-Type"] == "" {
				headers["Content-Type"] = ct
			}
		} else {
			formVals := neturl.Values{}
			for _, c := range route.BodyParams {
				formVals.Set(c.Key, c.GenerateValue())
			}
			body = []byte(formVals.Encode())
			if headers["Content-Type"] == "" {
				if ct != "" {
					headers["Content-Type"] = ct
				} else {
					headers["Content-Type"] = "application/x-www-form-urlencoded"
				}
			}
		}
	} else {
		body = r.Body
	}

	return buildRequest(method, parsed.String(), headers, body)
}

// buildFallback constructs a minimal request from the URL and method stored in r.
func (r *ReplayRequest) buildFallback() (*nethttp.Request, error) {
	return buildRequest(r.Method, r.URL, r.Headers, r.Body)
}

func buildRequest(method, rawURL string, headers map[string]string, body []byte) (*nethttp.Request, error) {
	var req *nethttp.Request
	var err error
	if len(body) > 0 {
		req, err = nethttp.NewRequest(method, rawURL, bytes.NewReader(body))
	} else {
		req, err = nethttp.NewRequest(method, rawURL, nethttp.NoBody)
	}
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// Execute reconstructs and sends the HTTP request using client.
// The WordlistPaths field is used for KSUID lookup during reconstruction.
func (r *ReplayRequest) Execute(client *nethttp.Client) (*ReplayResponse, error) {
	req, err := r.Reconstruct(r.WordlistPaths)
	if err != nil {
		return nil, fmt.Errorf("reconstruct: %w", err)
	}

	// Capture body bytes for raw-request display without consuming req.Body.
	var bodyBytes []byte
	if req.GetBody != nil {
		br, gberr := req.GetBody()
		if gberr == nil {
			bodyBytes, _ = io.ReadAll(br)
			br.Close()
		}
	}

	rawReq := formatRawRequest(req, bodyBytes)

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute: %w", err)
	}
	defer resp.Body.Close()
	duration := time.Since(start)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return &ReplayResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header.Clone(),
		Body:       respBody,
		Duration:   duration,
		RawRequest: rawReq,
	}, nil
}

// formatRawRequest formats an HTTP request as a raw HTTP/1.1 wire representation.
func formatRawRequest(req *nethttp.Request, body []byte) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s HTTP/1.1\r\n", req.Method, req.URL.RequestURI())
	fmt.Fprintf(&sb, "Host: %s\r\n", req.URL.Host)
	for k, vs := range req.Header {
		for _, v := range vs {
			fmt.Fprintf(&sb, "%s: %s\r\n", k, v)
		}
	}
	sb.WriteString("\r\n")
	if len(body) > 0 {
		sb.Write(body)
	}
	return sb.String()
}

func hasBody(method string) bool {
	switch method {
	case nethttp.MethodPost, nethttp.MethodPut, nethttp.MethodPatch:
		return true
	}
	return false
}
