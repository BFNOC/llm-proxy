package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type slidingWindow struct {
	timestamps []time.Time
}

func (sw *slidingWindow) countInWindow(now time.Time) int {
	cutoff := now.Add(-time.Minute)
	valid := 0
	for _, t := range sw.timestamps {
		if t.After(cutoff) {
			sw.timestamps[valid] = t
			valid++
		}
	}
	sw.timestamps = sw.timestamps[:valid]
	return valid
}

func (sw *slidingWindow) add(now time.Time) {
	sw.timestamps = append(sw.timestamps, now)
}

// PerKeyRPMLimiter tracks per-key requests per minute using a sliding window.
type PerKeyRPMLimiter struct {
	mu      sync.Mutex
	windows map[int64]*slidingWindow
}

func NewPerKeyRPMLimiter() *PerKeyRPMLimiter {
	return &PerKeyRPMLimiter{
		windows: make(map[int64]*slidingWindow),
	}
}

// Check returns (allowed, retryAfterSeconds).
// rpm=0 means unlimited.
func (l *PerKeyRPMLimiter) Check(keyID int64, rpm int) (bool, int) {
	if rpm <= 0 {
		return true, 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	sw, ok := l.windows[keyID]
	if !ok {
		sw = &slidingWindow{}
		l.windows[keyID] = sw
	}

	count := sw.countInWindow(now)
	if count >= rpm {
		retryAfter := 60 // worst case
		if len(sw.timestamps) > 0 {
			oldest := sw.timestamps[0]
			retryAfter = int(time.Until(oldest.Add(time.Minute)).Seconds()) + 1
			if retryAfter < 1 {
				retryAfter = 1
			}
		}
		return false, retryAfter
	}

	sw.add(now)
	return true, 0
}

// RemoveKey cleans up the window for a deleted key.
func (l *PerKeyRPMLimiter) RemoveKey(keyID int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.windows, keyID)
}

// RateLimitMiddleware applies per-key RPM limiting using a sliding window.
func RateLimitMiddleware(limiter *PerKeyRPMLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resolved := ResolvedKeyFromContext(r.Context())
			if resolved == nil {
				next.ServeHTTP(w, r)
				return
			}

			allowed, retryAfter := limiter.Check(resolved.ID, resolved.RPMLimit)
			if !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", resolved.RPMLimit))
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
					"error": "rate limit exceeded",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
