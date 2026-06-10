package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Default login rate-limit policy. Per the NekoLc API specification (v0.0.3+),
// rate limits should be applied to /v0/api/auth/login to mitigate brute-force
// attacks. These defaults can be adjusted per deployment.
const (
	loginRateLimitMax    = 10
	loginRateLimitWindow = time.Minute
)

// Default policy for sensitive, unauthenticated account endpoints (registration,
// password reset, email verification). These are kept stricter than login to
// curb email-sending abuse, token brute-forcing and account enumeration.
const (
	accountRateLimitMax    = 5
	accountRateLimitWindow = time.Minute
)

// rateLimiter is a simple in-memory sliding-window rate limiter keyed by an
// arbitrary string (typically the client IP).
type rateLimiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	attempts map[string][]time.Time
}

// newRateLimiter creates a rate limiter that allows at most max events per
// window for each key.
func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		max:      max,
		window:   window,
		attempts: make(map[string][]time.Time),
	}
}

// Allow records an attempt for the given key and reports whether it is within
// the configured limit. A return value of false means the limit was exceeded.
func (rl *rateLimiter) Allow(key string) bool {
	if rl == nil || rl.max <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-rl.window)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	recent := rl.attempts[key][:0]
	for _, ts := range rl.attempts[key] {
		if ts.After(cutoff) {
			recent = append(recent, ts)
		}
	}

	if len(recent) >= rl.max {
		rl.attempts[key] = recent
		return false
	}

	recent = append(recent, now)
	rl.attempts[key] = recent
	return true
}

// clientIP extracts a best-effort client identifier from the request. The
// chi RealIP middleware normalizes RemoteAddr from X-Forwarded-For/X-Real-IP
// when present, so RemoteAddr is used as the primary source.
func clientIP(r *http.Request) string {
	addr := strings.TrimSpace(r.RemoteAddr)
	if addr == "" {
		return "unknown"
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}
