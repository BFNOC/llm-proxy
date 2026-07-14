package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// GlobalRequestCounter
// ---------------------------------------------------------------------------

func TestGlobalRequestCounter_Increment_And_RPM(t *testing.T) {
	c := NewGlobalRequestCounter()

	assert.Equal(t, 0, c.RPM(), "fresh counter should have RPM 0")

	c.Increment()
	c.Increment()
	c.Increment()

	rpm := c.RPM()
	assert.Equal(t, 3, rpm, "RPM should reflect 3 increments")
}

func TestGlobalRequestCounter_RPS(t *testing.T) {
	c := NewGlobalRequestCounter()

	// Fire some requests so the current-second bucket has data.
	for i := 0; i < 10; i++ {
		c.Increment()
	}

	rps := c.RPS()
	// Cold-start window clamps to at least 1 second, so RPS should be
	// between 1.0 and 10.0 (depends on elapsed fraction of second).
	assert.GreaterOrEqual(t, rps, 1.0, "RPS should be at least 1.0")
	assert.LessOrEqual(t, rps, 11.0, "RPS should not exceed total requests + margin")
}

func TestGlobalRequestCounter_MultipleIncrements(t *testing.T) {
	c := NewGlobalRequestCounter()

	n := 100
	for i := 0; i < n; i++ {
		c.Increment()
	}

	rpm := c.RPM()
	assert.Equal(t, n, rpm)
}

// ---------------------------------------------------------------------------
// PerKeyStatsCollector
// ---------------------------------------------------------------------------

func TestPerKeyStatsCollector_Record_And_GetKeyRPM(t *testing.T) {
	sc := NewPerKeyStatsCollector()

	sc.Record(1)
	sc.Record(1)
	sc.Record(1)
	sc.Record(2)

	assert.Equal(t, 3, sc.GetKeyRPM(1))
	assert.Equal(t, 1, sc.GetKeyRPM(2))
	assert.Equal(t, 0, sc.GetKeyRPM(999), "unknown key should have RPM 0")
}

func TestPerKeyStatsCollector_AllActiveRPMs(t *testing.T) {
	sc := NewPerKeyStatsCollector()

	sc.Record(10)
	sc.Record(10)
	sc.Record(20)

	active := sc.AllActiveRPMs()
	assert.Equal(t, 2, active[10])
	assert.Equal(t, 1, active[20])
	_, has999 := active[999]
	assert.False(t, has999, "inactive key should not appear")
}

func TestPerKeyStatsCollector_RemoveKey(t *testing.T) {
	sc := NewPerKeyStatsCollector()

	sc.Record(5)
	sc.Record(5)
	require.Equal(t, 2, sc.GetKeyRPM(5))

	sc.RemoveKey(5)
	assert.Equal(t, 0, sc.GetKeyRPM(5), "RPM should be 0 after RemoveKey")

	// Key should not appear in AllActiveRPMs either.
	active := sc.AllActiveRPMs()
	_, has5 := active[5]
	assert.False(t, has5)
}

func TestPerKeyStatsCollector_RemoveKey_Nonexistent(t *testing.T) {
	sc := NewPerKeyStatsCollector()
	// Should not panic.
	sc.RemoveKey(12345)
}

func TestPerKeyStatsCollector_ConcurrentRecord(t *testing.T) {
	sc := NewPerKeyStatsCollector()
	done := make(chan struct{})

	// Hammer the same key from multiple goroutines.
	for g := 0; g < 4; g++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 50; i++ {
				sc.Record(7)
			}
		}()
	}
	for g := 0; g < 4; g++ {
		<-done
	}

	assert.Equal(t, 200, sc.GetKeyRPM(7))
}

// ---------------------------------------------------------------------------
// StatsMiddleware
// ---------------------------------------------------------------------------

func TestStatsMiddleware_IncrementsCounter(t *testing.T) {
	c := NewGlobalRequestCounter()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := StatsMiddleware(c)(next)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	assert.Equal(t, 5, c.RPM())
}

func TestStatsMiddleware_PassesThroughToNext(t *testing.T) {
	c := NewGlobalRequestCounter()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("created")) //nolint:errcheck
	})

	handler := StatsMiddleware(c)(next)
	req := httptest.NewRequest(http.MethodPost, "/v1/completions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "created", rec.Body.String())
}

// ---------------------------------------------------------------------------
// PerKeyStatsMiddleware
// ---------------------------------------------------------------------------

func TestPerKeyStatsMiddleware_RecordsWhenKeyPresent(t *testing.T) {
	sc := NewPerKeyStatsCollector()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := PerKeyStatsMiddleware(sc)(next)

	dk := &store.DownstreamKey{ID: 42, Name: "test-key", RPMLimit: 100, Enabled: true}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx := context.WithValue(req.Context(), ctxKeyResolvedKey, dk)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 1, sc.GetKeyRPM(42))
}

func TestPerKeyStatsMiddleware_SkipsWhenNoKey(t *testing.T) {
	sc := NewPerKeyStatsCollector()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := PerKeyStatsMiddleware(sc)(next)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called, "next handler should still be called")
	assert.Equal(t, http.StatusOK, rec.Code)

	// No keys should have any RPM recorded.
	active := sc.AllActiveRPMs()
	assert.Empty(t, active)
}

func TestPerKeyStatsMiddleware_MultipleKeys(t *testing.T) {
	sc := NewPerKeyStatsCollector()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := PerKeyStatsMiddleware(sc)(next)

	keys := []int64{10, 20, 10, 10, 20}
	for _, keyID := range keys {
		dk := &store.DownstreamKey{ID: keyID, Enabled: true}
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		ctx := context.WithValue(req.Context(), ctxKeyResolvedKey, dk)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	assert.Equal(t, 3, sc.GetKeyRPM(10))
	assert.Equal(t, 2, sc.GetKeyRPM(20))
}
