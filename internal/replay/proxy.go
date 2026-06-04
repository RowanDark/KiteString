package replay

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
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

	tlsCfg := &tls.Config{ //nolint:gosec // InsecureSkipVerify is required for interception proxies like Burp Suite; value is user-controlled
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
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			// Wrap the legacy Dial func in a ContextDialer-compatible closure.
			contextDialer = &dialerContextAdapter{dialer}
		}
		transport = &nethttp.Transport{
			DialContext:     contextDialer.DialContext,
			TLSClientConfig: tlsCfg,
		}

	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q (want http, https, or socks5)", parsed.Scheme)
	}

	return &nethttp.Client{Transport: transport}, nil
}

// dialerContextAdapter wraps a legacy proxy.Dialer to implement proxy.ContextDialer.
type dialerContextAdapter struct {
	d proxy.Dialer
}

func (a *dialerContextAdapter) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	// Run the blocking dial in a goroutine so context cancellation is respected.
	ch := make(chan struct {
		conn net.Conn
		err  error
	}, 1)
	go func() {
		conn, err := a.d.Dial(network, addr)
		ch <- struct {
			conn net.Conn
			err  error
		}{conn, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.conn, res.err
	}
}
