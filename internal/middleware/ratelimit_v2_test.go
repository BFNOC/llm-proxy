package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// PerKeyRPMLimiter unit tests
// ---------------------------------------------------------------------------

func TestNewPerKeyRPMLimiter(t *testing.T) {
	l := NewPerKeyRPMLimiter()
	defer l.StopGC()

	// All 64 shards should have initialised bucket maps.
	for i := 0; i < rpmLimiterShards; i++ {
		assert.NotNil(t, l.shards[i].buckets, "shard %d should have non-nil buckets", i)
	}
}

func TestCheck_UnlimitedPassesAlways(t *testing.T) {
	l := NewPerKeyRPMLimiter()
	defer l.StopGC()

	for i := 0; i < 200; i++ {
		allowed, retryAfter := l.Check(1, 0) // rpm=0 → unlimited
		assert.True(t, allowed, "unlimited key should always be allowed")
		assert.Equal(t, 0, retryAfter)
	}
}

func TestCheck_NegativeRPMTreatedAsUnlimited(t *testing.T) {
	l := NewPerKeyRPMLimiter()
	defer l.StopGC()

	allowed, retryAfter := l.Check(1, -5)
	assert.True(t, allowed)
	assert.Equal(t, 0, retryAfter)
}

func TestCheck_AllowsWithinLimit(t *testing.T) {
	l := NewPerKeyRPMLimiter()
	defer l.StopGC()

	rpm := 5
	for i := 0; i < rpm; i++ {
		allowed, retryAfter := l.Check(42, rpm)
		assert.True(t, allowed, "request %d should be allowed within limit %d", i+1, rpm)
		assert.Equal(t, 0, retryAfter)
	}
}

func TestCheck_BlocksAtLimit(t *testing.T) {
	l := NewPerKeyRPMLimiter()
	defer l.StopGC()

	rpm := 3
	for i := 0; i < rpm; i++ {
		allowed, _ := l.Check(42, rpm)
		require.True(t, allowed, "request %d should be allowed", i+1)
	}

	// Next request should be blocked.
	allowed, retryAfter := l.Check(42, rpm)
	assert.False(t, allowed, "request beyond limit should be blocked")
	assert.Greater(t, retryAfter, 0, "retryAfter should be positive")
	assert.LessOrEqual(t, retryAfter, 61, "retryAfter should be at most 61 seconds")
}

func TestCheck_DifferentKeysDontInterfere(t *testing.T) {
	l := NewPerKeyRPMLimiter()
	defer l.StopGC()

	rpm := 2

	// Exhaust key 1.
	for i := 0; i < rpm; i++ {
		l.Check(1, rpm)
	}
	allowed1, _ := l.Check(1, rpm)
	assert.False(t, allowed1, "key 1 should be blocked")

	// Key 2 should still be fine.
	allowed2, retryAfter := l.Check(2, rpm)
	assert.True(t, allowed2, "key 2 should not be affected by key 1")
	assert.Equal(t, 0, retryAfter)
}

func TestCheck_ShardIsolation(t *testing.T) {
	l := NewPerKeyRPMLimiter()
	defer l.StopGC()

	// Pick two IDs that map to different shards.
	keyA := int64(0)
	keyB := int64(1)
	assert.NotEqual(t, keyA%rpmLimiterShards, keyB%rpmLimiterShards,
		"test assumes keys land in different shards")

	rpm := 1
	l.Check(keyA, rpm)

	// keyA exhausted, keyB should be independent.
	allowedA, _ := l.Check(keyA, rpm)
	assert.False(t, allowedA)

	allowedB, _ := l.Check(keyB, rpm)
	assert.True(t, allowedB)
}

func TestRemoveKey(t *testing.T) {
	l := NewPerKeyRPMLimiter()
	defer l.StopGC()

	rpm := 2
	for i := 0; i < rpm; i++ {
		l.Check(99, rpm)
	}
	// Key 99 is now at limit.
	blocked, _ := l.Check(99, rpm)
	assert.False(t, blocked)

	// Remove and verify the slate is clean.
	l.RemoveKey(99)

	allowed, retryAfter := l.Check(99, rpm)
	assert.True(t, allowed, "after RemoveKey the counter should be reset")
	assert.Equal(t, 0, retryAfter)
}

func TestStopGC_NoPanic(t *testing.T) {
	l := NewPerKeyRPMLimiter()

	// Should not panic.
	l.StopGC()

	// Calling StopGC a second time should also be safe (sync.Once).
	l.StopGC()
}

func TestGC_DropsIdleWindows(t *testing.T) {
	l := NewPerKeyRPMLimiter()
	defer l.StopGC()

	// Record some traffic so that a window exists.
	l.Check(7, 100)

	s := l.shard(7)
	s.mu.Lock()
	_, exists := s.buckets[7]
	s.mu.Unlock()
	assert.True(t, exists, "window should exist after Check")

	// Manually expire all timestamps by clearing the slice, then run GC.
	s.mu.Lock()
	sw := s.buckets[7]
	sw.timestamps = sw.timestamps[:0]
	s.mu.Unlock()

	l.GC()

	s.mu.Lock()
	_, exists = s.buckets[7]
	s.mu.Unlock()
	assert.False(t, exists, "GC should drop the empty window")
}

// ---------------------------------------------------------------------------
// RateLimitMiddleware integration tests
// ---------------------------------------------------------------------------

// withResolvedKey injects a store.DownstreamKey into the request context,
// matching what KeyResolverMiddleware does at runtime.
func withResolvedKey(r *http.Request, dk *store.DownstreamKey) *http.Request {
	ctx := context.WithValue(r.Context(), ctxKeyResolvedKey, dk)
	return r.WithContext(ctx)
}

func TestRateLimitMiddleware_NoResolvedKey_Passes(t *testing.T) {
	l := NewPerKeyRPMLimiter()
	defer l.StopGC()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := RateLimitMiddleware(l)(next)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called, "next handler should be called when no resolved key")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRateLimitMiddleware_UnlimitedKey_Passes(t *testing.T) {
	l := NewPerKeyRPMLimiter()
	defer l.StopGC()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := RateLimitMiddleware(l)(next)
	dk := &store.DownstreamKey{ID: 10, RPMLimit: 0}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = withResolvedKey(req, dk)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRateLimitMiddleware_OverLimit_Returns429(t *testing.T) {
	l := NewPerKeyRPMLimiter()
	defer l.StopGC()

	dk := &store.DownstreamKey{ID: 20, RPMLimit: 2}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RateLimitMiddleware(l)(next)

	// Exhaust the limit.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req = withResolvedKey(req, dk)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "request %d should pass", i+1)
	}

	// Third request should be rate-limited.
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = withResolvedKey(req, dk)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"), "should set Retry-After header")
	assert.Equal(t, "2", rec.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "0", rec.Header().Get("X-RateLimit-Remaining"))
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]string
	err := json.Unmarshal(rec.Body.Bytes(), &body)
	require.NoError(t, err)
	assert.Contains(t, body["error"], "rate limit exceeded")
}
