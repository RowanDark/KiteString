package proute

import (
	"fmt"
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
