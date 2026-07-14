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

const rpmLimiterShards = 64

type rpmShard struct {
	mu      sync.Mutex
	buckets map[int64]*slidingWindow
}

// PerKeyRPMLimiter tracks per-key requests per minute using a sliding window.
type PerKeyRPMLimiter struct {
	shards [rpmLimiterShards]rpmShard
	stopGC chan struct{}
	gcOnce sync.Once
}

func (l *PerKeyRPMLimiter) shard(keyID int64) *rpmShard {
	idx := keyID % rpmLimiterShards
	if idx < 0 {
		idx = -idx
	}
	return &l.shards[idx]
}

func NewPerKeyRPMLimiter() *PerKeyRPMLimiter {
	l := &PerKeyRPMLimiter{
		stopGC: make(chan struct{}),
	}
	for i := range l.shards {
		l.shards[i].buckets = make(map[int64]*slidingWindow)
	}
	go l.gcLoop()
	return l
}

func (l *PerKeyRPMLimiter) gcLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.GC()
		case <-l.stopGC:
			return
		}
	}
}

// GC drops idle windows with no timestamps in the last minute.
func (l *PerKeyRPMLimiter) GC() {
	now := time.Now()
	for i := range l.shards {
		s := &l.shards[i]
		s.mu.Lock()
		for id, sw := range s.buckets {
			if sw.countInWindow(now) == 0 {
				delete(s.buckets, id)
			}
		}
		s.mu.Unlock()
	}
}

// StopGC stops the background GC goroutine (for tests/shutdown).
func (l *PerKeyRPMLimiter) StopGC() {
	l.gcOnce.Do(func() {
		close(l.stopGC)
	})
}

// Check returns (allowed, retryAfterSeconds).
// rpm=0 means unlimited.
func (l *PerKeyRPMLimiter) Check(keyID int64, rpm int) (bool, int) {
	if rpm <= 0 {
		return true, 0
	}

	s := l.shard(keyID)
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	sw, ok := s.buckets[keyID]
	if !ok {
		sw = &slidingWindow{}
		s.buckets[keyID] = sw
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
	s := l.shard(keyID)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.buckets, keyID)
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
