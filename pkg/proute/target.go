package proute

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

// ParseTarget parses one or more targets from an input string.
// Bare domains (no scheme) expand to both http://<host>:80 and https://<host>:443.
// URIs with an explicit scheme produce a single target.
func ParseTarget(input string) ([]ScanTarget, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("empty target")
	}

	// If no scheme present, expand to http:80 + https:443.
	if !strings.Contains(input, "://") {
		// Might have a port already (host:port/path).
		host, port, basePath, err := splitHostPortPath(input)
		if err != nil {
			return nil, err
		}
		if port != 0 {
			// Explicit port — single target, infer scheme from port.
			scheme := inferScheme(port)
			return []ScanTarget{{
				Scheme:   scheme,
				Host:     host,
				Port:     port,
				BasePath: basePath,
				Raw:      fmt.Sprintf("%s://%s:%d%s", scheme, host, port, basePath),
			}}, nil
		}
		// No port — expand to both.
		http := ScanTarget{Scheme: "http", Host: host, Port: 80, BasePath: basePath,
			Raw: fmt.Sprintf("http://%s%s", host, basePath)}
		https := ScanTarget{Scheme: "https", Host: host, Port: 443, BasePath: basePath,
			Raw: fmt.Sprintf("https://%s%s", host, basePath)}
		return []ScanTarget{http, https}, nil
	}

	u, err := url.Parse(input)
	if err != nil {
		return nil, fmt.Errorf("invalid URI %q: %w", input, err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}

	hostname := u.Hostname()
	portStr := u.Port()
	var port int
	if portStr != "" {
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid port %q: %w", portStr, err)
		}
	} else {
		port = defaultPort(scheme)
	}

	basePath := u.Path
	if basePath == "" {
		basePath = "/"
	}

	raw := fmt.Sprintf("%s://%s:%d%s", scheme, hostname, port, basePath)
	if u.RawQuery != "" {
		raw += "?" + u.RawQuery
	}

	return []ScanTarget{{
		Scheme:   scheme,
		Host:     hostname,
		Port:     port,
		BasePath: basePath,
		Raw:      raw,
	}}, nil
}

func splitHostPortPath(input string) (host string, port int, basePath string, err error) {
	// Separate path component.
	slashIdx := strings.Index(input, "/")
	var hostPart, pathPart string
	if slashIdx >= 0 {
		hostPart = input[:slashIdx]
		pathPart = input[slashIdx:]
	} else {
		hostPart = input
		pathPart = "/"
	}

	// Check for port in hostPart.
	colonIdx := strings.LastIndex(hostPart, ":")
	if colonIdx >= 0 {
		portVal, parseErr := strconv.Atoi(hostPart[colonIdx+1:])
		if parseErr == nil {
			return hostPart[:colonIdx], portVal, pathPart, nil
		}
	}
	return hostPart, 0, pathPart, nil
}

func defaultPort(scheme string) int {
	if scheme == "https" {
		return 443
	}
	return 80
}

func inferScheme(port int) string {
	if port == 443 || port == 8443 {
		return "https"
	}
	return "http"
}

// httpxJSONLine is the shape of a single httpx -json output object.
type httpxJSONLine struct {
	URL        string   `json:"url"`
	StatusCode int      `json:"status_code"`
	Title      string   `json:"title"`
	Tech       []string `json:"tech"`
}

// ParseInputLine detects the input format of a single line and returns a ScanTarget.
// Returns nil, nil for blank lines and lines beginning with '#'.
// Supported formats:
//   - httpx JSON:     {"url":"https://...","status_code":200,"tech":["nginx"]}
//   - httpx standard: https://example.com [200] [Title] [nginx,php]
//   - Plain URL:      https://example.com
//   - Plain host:     example.com
func ParseInputLine(line string) (*ScanTarget, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil, nil
	}

	if strings.HasPrefix(line, "{") {
		return parseHTTPXJSON(line)
	}

	// httpx standard format: URL followed by space and bracket metadata
	if idx := strings.Index(line, " ["); idx > 0 && strings.Contains(line[:idx], "://") {
		return parseHTTPXStandard(line, idx)
	}

	targets, err := ParseTarget(line)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, nil
	}
	return &targets[0], nil
}

// ParseInputStream reads all newline-delimited lines from r, calling ParseInputLine
// on each, and returns the collected targets. Blank lines and comments are skipped.
func ParseInputStream(r io.Reader) ([]ScanTarget, error) {
	sc := bufio.NewScanner(r)
	var targets []ScanTarget
	for sc.Scan() {
		t, err := ParseInputLine(sc.Text())
		if err != nil {
			return nil, err
		}
		if t != nil {
			targets = append(targets, *t)
		}
	}
	return targets, sc.Err()
}

func parseHTTPXJSON(line string) (*ScanTarget, error) {
	var j httpxJSONLine
	if err := json.Unmarshal([]byte(line), &j); err != nil {
		return nil, fmt.Errorf("invalid JSON input: %w", err)
	}
	if j.URL == "" {
		return nil, fmt.Errorf("JSON line missing url field")
	}
	targets, err := ParseTarget(j.URL)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, nil
	}
	t := targets[0]
	t.Tags = j.Tech
	return &t, nil
}

func parseHTTPXStandard(line string, splitIdx int) (*ScanTarget, error) {
	urlPart := strings.TrimSpace(line[:splitIdx])
	rest := line[splitIdx:]

	// Collect all bracket group contents.
	var groups []string
	for i := 0; i < len(rest); {
		open := strings.Index(rest[i:], "[")
		if open < 0 {
			break
		}
		open += i
		closeIdx := strings.Index(rest[open:], "]")
		if closeIdx < 0 {
			break
		}
		closeIdx += open
		groups = append(groups, rest[open+1:closeIdx])
		i = closeIdx + 1
	}

	targets, err := ParseTarget(urlPart)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, nil
	}
	t := targets[0]

	// httpx standard output order: [status] [title] [tech]
	// Extract tech from last group when 3+ groups are present.
	if len(groups) >= 3 {
		last := groups[len(groups)-1]
		parts := strings.Split(last, ",")
		for i, p := range parts {
			parts[i] = strings.TrimSpace(p)
		}
		t.Tags = parts
	}

	return &t, nil
}
