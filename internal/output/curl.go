package output

import (
	"encoding/json"
	"strings"

	"github.com/RowanDark/kitestring/pkg/proute"
)

// GenerateCurl produces a complete, executable curl one-liner reproducing the request.
func GenerateCurl(result proute.ScanResult) string {
	var parts []string
	parts = append(parts, "curl")

	method := result.Route.Method
	if method == "" {
		method = "GET"
	}
	parts = append(parts, "-X", method)

	// URL (GET query params are already embedded by the scanner)
	parts = append(parts, result.URL)

	// Content-Type header
	ct := result.Route.ContentType
	if ct == "" && isBodyMethod(method) && len(result.Route.BodyParams) > 0 {
		ct = "application/json"
	}
	if ct != "" {
		parts = append(parts, "-H", shellQuote("Content-Type: "+ct))
	}

	// Custom route headers
	for _, h := range result.Route.Headers {
		parts = append(parts, "-H", shellQuote(h.Key+": "+h.GenerateValue()))
	}

	// Body for mutating methods
	if isBodyMethod(method) && len(result.Route.BodyParams) > 0 {
		body := buildJSONBody(result.Route.BodyParams)
		parts = append(parts, "-d", shellQuote(body))
	}

	return strings.Join(parts, " ")
}

func isBodyMethod(method string) bool {
	switch strings.ToUpper(method) {
	case "POST", "PUT", "PATCH":
		return true
	}
	return false
}

func buildJSONBody(params []proute.Crumb) string {
	obj := make(map[string]string, len(params))
	for _, p := range params {
		obj[p.Key] = p.GenerateValue()
	}
	b, _ := json.Marshal(obj)
	return string(b)
}

// shellQuote wraps s in single quotes, escaping any internal single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
