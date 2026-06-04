package replay

import (
	"crypto/tls"
	"fmt"
	nethttp "net/http"
	neturl "net/url"

	"golang.org/x/net/proxy"
)

// NewProxyClient creates an HTTP client that routes all traffic through proxyURL.
// Supported URL schemes: http, https, socks5.
// When tlsSkipVerify is true, TLS certificate verification is disabled (required
// for interception proxies like Burp Suite or OWASP ZAP).
func NewProxyClient(proxyURL string, tlsSkipVerify bool) (*nethttp.Client, error) {
	parsed, err := neturl.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("parse proxy URL: %w", err)
	}

	tlsCfg := &tls.Config{ //nolint:gosec // InsecureSkipVerify is required for interception proxies like Burp Suite
		InsecureSkipVerify: tlsSkipVerify,
	}

	var transport *nethttp.Transport

	switch parsed.Scheme {
	case "http", "https":
		transport = &nethttp.Transport{
			Proxy:           nethttp.ProxyURL(parsed),
			TLSClientConfig: tlsCfg,
		}

	case "socks5", "socks5h":
		var auth *proxy.Auth
		if parsed.User != nil {
			pass, _ := parsed.User.Password()
			auth = &proxy.Auth{
				User:     parsed.User.Username(),
				Password: pass,
			}
		}
		dialer, dialErr := proxy.SOCKS5("tcp", parsed.Host, auth, proxy.Direct)
		if dialErr != nil {
			return nil, fmt.Errorf("create SOCKS5 dialer: %w", dialErr)
		}
		transport = &nethttp.Transport{
			Dial:            dialer.Dial,
			TLSClientConfig: tlsCfg,
		}

	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q (want http, https, or socks5)", parsed.Scheme)
	}

	return &nethttp.Client{Transport: transport}, nil
}
