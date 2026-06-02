package http

import (
	"context"
	"sync"
	"time"
)

const (
	defaultBaseBackoff = 5 * time.Second
	defaultMaxBackoff  = 60 * time.Second
	defaultMaxRetries  = 3
)

// hostState tracks per-host request timing and 429 backoff counter.
type hostState struct {
	mu          sync.Mutex
	lastRequest time.Time
	backoffN    int // consecutive 429s; reset on any non-429 success
}

// RateLimiter enforces per-host request pacing and exponential 429 backoff.
type RateLimiter struct {
	delay       time.Duration
	baseBackoff time.Duration
	maxBackoff  time.Duration
	maxRetries  int
	hosts       sync.Map // map[string]*hostState
}

// RateLimiterConfig holds creation parameters for a RateLimiter.
type RateLimiterConfig struct {
	// Delay is the minimum gap between successive requests to the same host.
	Delay time.Duration
	// MaxRetries is the number of 429-triggered retries before giving up.
	MaxRetries int
	// BaseBackoff is the initial backoff on the first 429 (default: 5s).
	BaseBackoff time.Duration
	// MaxBackoff caps the exponential backoff (default: 60s).
	MaxBackoff time.Duration
}

// NewRateLimiter returns a RateLimiter with the given config.
func NewRateLimiter(cfg RateLimiterConfig) *RateLimiter {
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = defaultBaseBackoff
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = defaultMaxBackoff
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = defaultMaxRetries
	}
	return &RateLimiter{
		delay:       cfg.Delay,
		baseBackoff: cfg.BaseBackoff,
		maxBackoff:  cfg.MaxBackoff,
		maxRetries:  cfg.MaxRetries,
	}
}

// Wait blocks until the configured inter-request delay has elapsed for host.
// The wait is interruptible: if ctx is cancelled before the delay expires,
// Wait returns ctx.Err() immediately.
func (rl *RateLimiter) Wait(host string, ctx context.Context) error {
	if rl.delay <= 0 {
		return nil
	}
	state := rl.state(host)
	state.mu.Lock()
	wait := rl.delay - time.Since(state.lastRequest)
	if wait > 0 {
		state.lastRequest = time.Now().Add(wait)
	} else {
		state.lastRequest = time.Now()
		wait = 0
	}
	state.mu.Unlock()

	if wait <= 0 {
		return nil
	}
	select {
	case <-time.After(wait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// On429 records a 429 response for host and returns the duration to back off
// plus whether the caller should retry. Returns (0, false) when maxRetries is
// exceeded and the host should be skipped.
func (rl *RateLimiter) On429(host string, attempt int) (time.Duration, bool) {
	if attempt >= rl.maxRetries {
		return 0, false
	}
	state := rl.state(host)
	state.mu.Lock()
	n := state.backoffN
	state.backoffN++
	state.mu.Unlock()

	// Exponential: base * 2^n, capped at maxBackoff.
	backoff := rl.baseBackoff << uint(n)
	if backoff <= 0 || backoff > rl.maxBackoff {
		backoff = rl.maxBackoff
	}
	return backoff, true
}

// ResetBackoff clears the 429 counter for host after a successful response.
func (rl *RateLimiter) ResetBackoff(host string) {
	state := rl.state(host)
	state.mu.Lock()
	state.backoffN = 0
	state.mu.Unlock()
}

func (rl *RateLimiter) state(host string) *hostState {
	v, _ := rl.hosts.LoadOrStore(host, &hostState{})
	return v.(*hostState)
}
