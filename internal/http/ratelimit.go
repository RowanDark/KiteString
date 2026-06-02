package http

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrHostUnreachable is returned when a host has been marked unreachable after
// too many consecutive connection failures.
var ErrHostUnreachable = errors.New("host unreachable")

const (
	defaultBaseBackoff       = 5 * time.Second
	defaultMaxBackoff        = 60 * time.Second
	defaultMaxRetries        = 3
	defaultUnreachableThresh = 5
	defaultConnRetryDelay    = 2 * time.Second
)

// hostState tracks per-host request timing, 429 backoff, and failure counts.
type hostState struct {
	mu                  sync.Mutex
	lastRequest         time.Time
	backoffN            int // consecutive 429s; reset on any non-429 success
	consecutiveFailures int // connection failures; reset on success
	unreachable         bool
}

// RateLimiter enforces per-host request pacing, exponential 429 backoff, and
// host reachability tracking.
type RateLimiter struct {
	delay             time.Duration
	baseBackoff       time.Duration
	maxBackoff        time.Duration
	maxRetries        int
	unreachableThresh int
	connRetryDelay    time.Duration
	hosts             sync.Map // map[string]*hostState
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
	// UnreachableThreshold is the number of consecutive connection failures
	// before a host is marked unreachable (default: 5).
	UnreachableThreshold int
	// ConnRetryDelay is the pause before the single connection-error retry
	// (default: 2s). Tests may set this lower.
	ConnRetryDelay time.Duration
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
	if cfg.UnreachableThreshold <= 0 {
		cfg.UnreachableThreshold = defaultUnreachableThresh
	}
	if cfg.ConnRetryDelay <= 0 {
		cfg.ConnRetryDelay = defaultConnRetryDelay
	}
	return &RateLimiter{
		delay:             cfg.Delay,
		baseBackoff:       cfg.BaseBackoff,
		maxBackoff:        cfg.MaxBackoff,
		maxRetries:        cfg.MaxRetries,
		unreachableThresh: cfg.UnreachableThreshold,
		connRetryDelay:    cfg.ConnRetryDelay,
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

// Backoff returns the exponential backoff duration for the given attempt number:
// min(base * 2^attempt, maxBackoff). This is a pure calculation independent of
// accumulated per-host state.
func (rl *RateLimiter) Backoff(host string, attempt int) time.Duration {
	b := rl.baseBackoff << uint(attempt)
	if b <= 0 || b > rl.maxBackoff {
		return rl.maxBackoff
	}
	return b
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

	backoff := rl.baseBackoff << uint(n)
	if backoff <= 0 || backoff > rl.maxBackoff {
		backoff = rl.maxBackoff
	}
	return backoff, true
}

// RecordResponse updates the backoff counter for host based on statusCode.
// On 429 the counter is incremented; on any other status it is reset to zero.
func (rl *RateLimiter) RecordResponse(host string, statusCode int) {
	state := rl.state(host)
	state.mu.Lock()
	defer state.mu.Unlock()
	if statusCode == 429 {
		state.backoffN++
	} else {
		state.backoffN = 0
	}
}

// ResetBackoff clears the 429 counter for host after a successful response.
func (rl *RateLimiter) ResetBackoff(host string) {
	state := rl.state(host)
	state.mu.Lock()
	state.backoffN = 0
	state.mu.Unlock()
}

// RecordFailure records a connection failure for host and returns true if the
// host is now considered unreachable (consecutive failures >= threshold).
func (rl *RateLimiter) RecordFailure(host string) bool {
	state := rl.state(host)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.unreachable {
		return true
	}
	state.consecutiveFailures++
	if state.consecutiveFailures >= rl.unreachableThresh {
		state.unreachable = true
		return true
	}
	return false
}

// ResetFailures clears the consecutive failure counter for host on success.
func (rl *RateLimiter) ResetFailures(host string) {
	state := rl.state(host)
	state.mu.Lock()
	state.consecutiveFailures = 0
	state.mu.Unlock()
}

// IsUnreachable reports whether host has been marked unreachable.
func (rl *RateLimiter) IsUnreachable(host string) bool {
	v, ok := rl.hosts.Load(host)
	if !ok {
		return false
	}
	s := v.(*hostState)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unreachable
}

// ConnRetryDelay returns the delay to wait before the single connection-error retry.
func (rl *RateLimiter) ConnRetryDelay() time.Duration {
	return rl.connRetryDelay
}

func (rl *RateLimiter) state(host string) *hostState {
	v, _ := rl.hosts.LoadOrStore(host, &hostState{})
	return v.(*hostState)
}
