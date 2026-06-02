package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/RowanDark/kitestring/pkg/proute"
)

// ClientConfig holds creation parameters for a Client.
type ClientConfig struct {
	Timeout             time.Duration
	SkipTLSVerify       bool
	MaxIdleConnsPerHost int
	UserAgent           string
	MaxRedirects        int
	ExtraHeaders        map[string]string
	BlacklistDomains    []string             // do not follow redirects to these domains
	Scope               proute.ScopeChecker  // optional; blocks out-of-scope redirect destinations
	Verbose             string               // verbosity level for redirect-blocked log messages
}

// Client wraps net/http.Client with KiteString-specific defaults.
type Client struct {
	inner  *nethttp.Client
	config ClientConfig
}

// NewClient returns a Client configured with cfg.
func NewClient(cfg ClientConfig) *Client {
	if cfg.MaxIdleConnsPerHost <= 0 {
		cfg.MaxIdleConnsPerHost = 32
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "KiteString/1.0"
	}

	transport := &nethttp.Transport{
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		TLSClientConfig: &tls.Config{ //nolint:gosec
			InsecureSkipVerify: cfg.SkipTLSVerify,
		},
	}

	maxRedir := cfg.MaxRedirects
	blacklist := cfg.BlacklistDomains
	scopeChecker := cfg.Scope
	verbose := cfg.Verbose
	inner := &nethttp.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
		CheckRedirect: func(req *nethttp.Request, via []*nethttp.Request) error {
			if maxRedir <= 0 {
				return nethttp.ErrUseLastResponse
			}
			if len(via) >= maxRedir {
				return nethttp.ErrUseLastResponse
			}
			hostname := req.URL.Hostname()
			for _, domain := range blacklist {
				if hostname == domain || strings.HasSuffix(hostname, "."+domain) {
					return nethttp.ErrUseLastResponse
				}
			}
			if scopeChecker != nil && scopeChecker.IsOutOfScope(hostname) {
				if verbose == "warn" || verbose == "info" || verbose == "debug" || verbose == "trace" {
					log.Printf("[WARN] blocked out-of-scope redirect to %s", req.URL)
				}
				return nethttp.ErrUseLastResponse
			}
			return nil
		},
	}

	return &Client{inner: inner, config: cfg}
}

// Do executes req and returns a normalised Response.
func (c *Client) Do(req *Request) (*Response, error) {
	return c.DoContext(context.Background(), req)
}

// DoContext is like Do but honours ctx for cancellation.
// Used internally by the pool so in-flight requests respect pool shutdown.
func (c *Client) DoContext(ctx context.Context, req *Request) (*Response, error) {
	raw, err := req.toHTTPRequest()
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	raw = raw.WithContext(ctx)

	if c.config.UserAgent != "" {
		raw.Header.Set("User-Agent", c.config.UserAgent)
	}
	for k, v := range c.config.ExtraHeaders {
		raw.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := c.inner.Do(raw)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	return Normalize(resp, time.Since(start))
}
