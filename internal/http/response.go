package http

import (
	"fmt"
	"io"
	nethttp "net/http"
	"time"
)

const maxBodyRead = 10 << 20 // 10 MiB cap on body reads

// Response holds the normalised result of a single HTTP round-trip.
type Response struct {
	StatusCode    int
	ContentLength int64
	Body          []byte
	Headers       nethttp.Header
	Duration      time.Duration
	URL           string
}

// Normalize drains and caps the response body, then assembles a Response.
func Normalize(resp *nethttp.Response, duration time.Duration) (*Response, error) {
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyRead))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	cl := resp.ContentLength
	if cl < 0 {
		cl = int64(len(body))
	}

	return &Response{
		StatusCode:    resp.StatusCode,
		ContentLength: cl,
		Body:          body,
		Headers:       resp.Header.Clone(),
		Duration:      duration,
		URL:           resp.Request.URL.String(),
	}, nil
}
