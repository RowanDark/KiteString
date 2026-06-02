package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	nethttp "net/http"
	neturl "net/url"
	"strings"

	"github.com/RowanDark/kitestring/pkg/proute"
)

// Request holds everything needed to build and retry an HTTP request.
// Storing the body as []byte (rather than io.Reader) allows the underlying
// net/http.Request to be reconstructed on every attempt.
type Request struct {
	Route   proute.Route
	Target  proute.ScanTarget
	FullURL string
	Method  string
	Headers map[string]string
	Body    []byte // nil for bodyless methods
}

// Build constructs a Request from a Route and ScanTarget.
//
// Path placeholders ({name}) are replaced with generated values.
// Query parameters, headers, and the request body are populated from
// the route's Crumb fields via Crumb.GenerateValue().
func Build(route proute.Route, target proute.ScanTarget) (*Request, error) {
	method := strings.ToUpper(route.Method)
	if method == "" {
		method = nethttp.MethodGet
	}

	// Resolve path template → concrete path segment.
	path := resolvePath(route.Path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Assemble full URL: scheme://host[:port]<basePath><routePath>[?query]
	base := buildBaseURL(target)
	basePath := strings.TrimSuffix(target.BasePath, "/")
	rawURL := base + basePath + path

	if len(route.QueryParams) > 0 {
		vals := neturl.Values{}
		for _, c := range route.QueryParams {
			vals.Set(c.Key, c.GenerateValue())
		}
		rawURL += "?" + vals.Encode()
	}

	// Route-level headers: each Crumb becomes a header key/value pair.
	headers := make(map[string]string, len(route.Headers))
	for _, c := range route.Headers {
		headers[c.Key] = c.GenerateValue()
	}

	// Build request body for methods that semantically support one.
	var body []byte
	if len(route.BodyParams) > 0 && hasBody(method) {
		ct := route.ContentType
		if strings.Contains(ct, "application/json") {
			m := make(map[string]interface{}, len(route.BodyParams))
			for _, c := range route.BodyParams {
				m[c.Key] = c.GenerateValue()
			}
			var err error
			body, err = json.Marshal(m)
			if err != nil {
				return nil, fmt.Errorf("marshal json body: %w", err)
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
	}

	return &Request{
		Route:   route,
		Target:  target,
		FullURL: rawURL,
		Method:  method,
		Headers: headers,
		Body:    body,
	}, nil
}

// toHTTPRequest builds a fresh *net/http.Request from stored fields.
// It is called on every attempt so that retries always send a fresh body reader.
func (r *Request) toHTTPRequest() (*nethttp.Request, error) {
	var bodyBytes *bytes.Reader
	if len(r.Body) > 0 {
		bodyBytes = bytes.NewReader(r.Body)
	}

	var req *nethttp.Request
	var err error
	if bodyBytes != nil {
		req, err = nethttp.NewRequest(r.Method, r.FullURL, bodyBytes)
	} else {
		req, err = nethttp.NewRequest(r.Method, r.FullURL, nil)
	}
	if err != nil {
		return nil, err
	}

	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// resolvePath replaces {param} placeholders in a URL path template with
// randomly generated strings.
func resolvePath(path string) string {
	if !strings.Contains(path, "{") {
		return path
	}
	var b strings.Builder
	b.Grow(len(path))
	i := 0
	for i < len(path) {
		if path[i] != '{' {
			b.WriteByte(path[i])
			i++
			continue
		}
		end := strings.IndexByte(path[i:], '}')
		if end < 0 {
			b.WriteByte(path[i])
			i++
			continue
		}
		b.WriteString(proute.Crumb{Type: proute.CrumbRandomString}.GenerateValue())
		i += end + 1
	}
	return b.String()
}

// buildBaseURL returns scheme://host or scheme://host:port, omitting default ports.
func buildBaseURL(t proute.ScanTarget) string {
	isDefault := t.Port == 0 ||
		(t.Scheme == "http" && t.Port == 80) ||
		(t.Scheme == "https" && t.Port == 443)
	if isDefault {
		return t.Scheme + "://" + t.Host
	}
	return fmt.Sprintf("%s://%s:%d", t.Scheme, t.Host, t.Port)
}

// hasBody reports whether the HTTP method semantically supports a request body.
func hasBody(method string) bool {
	switch method {
	case nethttp.MethodPost, nethttp.MethodPut, nethttp.MethodPatch:
		return true
	}
	return false
}
