package middleware

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
)

// ConcurrencyLimiter 跟踪每个下游 Key 的并发请求数。
// 当 Key 设置了 max_concurrent > 0 且当前并发数达到上限时，返回 429。
type ConcurrencyLimiter struct {
	mu       sync.Mutex
	counters map[int64]*atomic.Int64 // keyID -> 当前并发计数
}

// NewConcurrencyLimiter 创建并发限制器。
func NewConcurrencyLimiter() *ConcurrencyLimiter {
	return &ConcurrencyLimiter{
		counters: make(map[int64]*atomic.Int64),
	}
}

// getCounter 返回指定 keyID 的计数器，如果不存在则创建。
func (cl *ConcurrencyLimiter) getCounter(keyID int64) *atomic.Int64 {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	c, ok := cl.counters[keyID]
	if !ok {
		c = &atomic.Int64{}
		cl.counters[keyID] = c
	}
	return c
}

// Acquire 递增指定 keyID 的并发计数。如果已达上限（limit > 0），返回 false。
func (cl *ConcurrencyLimiter) Acquire(keyID int64, limit int) bool {
	if limit <= 0 {
		return true
	}
	c := cl.getCounter(keyID)
	// 使用 CAS 循环避免超发：先加 1，超限则回退。
	newVal := c.Add(1)
	if newVal > int64(limit) {
		c.Add(-1)
		return false
	}
	return true
}

// Release 递减指定 keyID 的并发计数。
func (cl *ConcurrencyLimiter) Release(keyID int64) {
	cl.mu.Lock()
	c, ok := cl.counters[keyID]
	cl.mu.Unlock()
	if !ok {
		return
	}
	c.Add(-1)
}

// GetCount 返回指定 keyID 的当前并发请求数。
func (cl *ConcurrencyLimiter) GetCount(keyID int64) int64 {
	cl.mu.Lock()
	c, ok := cl.counters[keyID]
	cl.mu.Unlock()
	if !ok {
		return 0
	}
	return c.Load()
}

// ConcurrencyMiddleware 返回按下游 Key 的 MaxConcurrent 限制并发请求数的 HTTP 中间件。
// 超限时返回 429 及 JSON 错误信息。
func ConcurrencyMiddleware(limiter *ConcurrencyLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resolved := ResolvedKeyFromContext(r.Context())
			if resolved == nil || resolved.MaxConcurrent <= 0 {
				// 未鉴权或未设并发限制，直接放行。
				next.ServeHTTP(w, r)
				return
			}

			if !limiter.Acquire(resolved.ID, resolved.MaxConcurrent) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
					"error": "concurrent request limit exceeded",
				})
				return
			}
			defer limiter.Release(resolved.ID)

			next.ServeHTTP(w, r)
		})
	}
}
