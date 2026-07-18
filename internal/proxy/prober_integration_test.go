package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ProbeNow / GetCurrentID / ActiveRequests
// ---------------------------------------------------------------------------

func TestProbeNow_TriggersProbe(t *testing.T) {
	s := newTestStore(t)
	dp := NewDynamicProxy()

	healthy := healthyServer(t)
	_, err := s.CreateUpstream("probe-target", healthy.URL, []string{"pk"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	prober := NewUpstreamProber(s, dp, time.Minute, 5*time.Second)

	// Before ProbeNow, no upstreams should be active.
	assert.Nil(t, dp.GetActiveUpstream())

	prober.ProbeNow()

	// After ProbeNow, the healthy upstream should be active.
	all := dp.GetAllUpstreams()
	require.Len(t, all, 1)
	assert.Equal(t, "probe-target", all[0].Name)
}

func TestProbeNow_IncrementalProbes(t *testing.T) {
	s := newTestStore(t)
	dp := NewDynamicProxy()

	healthy := healthyServer(t)
	_, err := s.CreateUpstream("first", healthy.URL, []string{"k1"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	prober := NewUpstreamProber(s, dp, time.Minute, 5*time.Second)
	prober.ProbeNow()
	require.Len(t, dp.GetAllUpstreams(), 1)

	// Add another upstream and re-probe.
	healthy2 := healthyServer(t)
	_, err = s.CreateUpstream("second", healthy2.URL, []string{"k2"}, 1, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	prober.ProbeNow()
	assert.Len(t, dp.GetAllUpstreams(), 2, "second probe should pick up newly added upstream")
}

func TestGetCurrentID_ReturnsZero_Initially(t *testing.T) {
	s := newTestStore(t)
	dp := NewDynamicProxy()
	prober := NewUpstreamProber(s, dp, time.Minute, 5*time.Second)

	assert.Equal(t, int64(0), prober.GetCurrentID(), "should be 0 before any probe")
}

func TestGetCurrentID_ReturnsZero_AfterProbe(t *testing.T) {
	s := newTestStore(t)
	dp := NewDynamicProxy()

	healthy := healthyServer(t)
	_, err := s.CreateUpstream("test", healthy.URL, []string{"k"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	prober := NewUpstreamProber(s, dp, time.Minute, 5*time.Second)
	prober.ProbeNow()

	// In multi-upstream mode, currentID is always 0.
	assert.Equal(t, int64(0), prober.GetCurrentID())
}

func TestActiveRequests_InitiallyZero(t *testing.T) {
	dp := NewDynamicProxy()
	assert.Equal(t, int64(0), dp.ActiveRequests())
}

func TestActiveRequests_DuringRequest(t *testing.T) {
	dp := NewDynamicProxy()

	var captured int64
	hold := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.StoreInt64(&captured, dp.ActiveRequests())
		<-hold
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed, APIKeys: []string{"k"}, Name: "test"},
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		rec := httptest.NewRecorder()
		dp.ServeHTTP(rec, req)
	}()

	// Wait for the request to be in flight.
	assert.Eventually(t, func() bool {
		return atomic.LoadInt64(&captured) > 0
	}, 2*time.Second, 10*time.Millisecond)

	close(hold)
	<-done

	assert.Equal(t, int64(1), atomic.LoadInt64(&captured), "should have 1 active request during handler")
	assert.Equal(t, int64(0), dp.ActiveRequests(), "should be 0 after completion")
}

// ---------------------------------------------------------------------------
// X-Model header set from body
// ---------------------------------------------------------------------------

func TestServeHTTP_SetsXModelHeader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed, APIKeys: []string{"k"}, Name: "test"},
	})

	body := `{"model":"gpt-4o-mini","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "gpt-4o-mini", rec.Header().Get("X-Model"))
}

// ---------------------------------------------------------------------------
// Prober probeUpstream with disabled upstream
// ---------------------------------------------------------------------------

func TestProber_DisabledUpstreamsSkipped(t *testing.T) {
	s := newTestStore(t)
	dp := NewDynamicProxy()

	healthy := healthyServer(t)
	up, err := s.CreateUpstream("disabled", healthy.URL, []string{"k"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	// Disable the upstream via BatchSetUpstreamEnabled.
	_, err = s.BatchSetUpstreamEnabled([]int64{up.ID}, false)
	require.NoError(t, err)

	prober := NewUpstreamProber(s, dp, time.Minute, 5*time.Second)
	prober.ProbeNow()

	assert.Nil(t, dp.GetActiveUpstream(), "disabled upstream should be skipped")
}

// ---------------------------------------------------------------------------
// shouldCountKeyFailure coverage
// ---------------------------------------------------------------------------

func TestShouldCountKeyFailure(t *testing.T) {
	tests := []struct {
		kind FailureKind
		want bool
	}{
		{FailureAuth, true},
		{FailureRateLimit, true},
		{FailureQuota, true},
		{FailureServerError, false},
		{FailureNone, false},
	}
	for _, tc := range tests {
		t.Run(string(tc.kind), func(t *testing.T) {
			assert.Equal(t, tc.want, shouldCountKeyFailure(tc.kind))
		})
	}
}

// ---------------------------------------------------------------------------
// GET request skips model extraction
// ---------------------------------------------------------------------------

func TestServeHTTP_GetRequest_SkipsModelExtraction(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"object":"list"}`))
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed, APIKeys: []string{"k"}, Name: "test", ModelPatterns: []string{"claude-*"}},
	})

	// GET request should not be filtered by model patterns.
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "GET should bypass model filtering")
}

// ---------------------------------------------------------------------------
// ClearActiveUpstream
// ---------------------------------------------------------------------------

func TestClearActiveUpstream(t *testing.T) {
	dp := NewDynamicProxy()
	parsed, _ := url.Parse("http://example.com")
	dp.SetActiveUpstream(parsed, "key", "test")
	require.NotNil(t, dp.GetActiveUpstream())

	dp.ClearActiveUpstream()
	assert.Nil(t, dp.GetActiveUpstream())
	assert.Empty(t, dp.GetAllUpstreams())
}

// ---------------------------------------------------------------------------
// Last upstream error (no failover possible)
// ---------------------------------------------------------------------------

func TestServeHTTP_LastUpstream_Error_Returns502(t *testing.T) {
	// Point at an address that refuses connections (single upstream, no failover).
	dp := NewDynamicProxy()
	parsed, _ := url.Parse("http://127.0.0.1:1")
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed, APIKeys: []string{"k"}, KeyRowIDs: []int64{10}, Name: "dead"},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body["error"], "bad gateway")
}

// ---------------------------------------------------------------------------
// All upstreams fail -> returns last upstream error
// ---------------------------------------------------------------------------

func TestServeHTTP_AllUpstreamsFail_ReturnsLastError(t *testing.T) {
	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited 1"}`))
	}))
	defer upstream1.Close()

	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited 2"}`))
	}))
	defer upstream2.Close()

	dp := NewDynamicProxy()
	parsed1, _ := url.Parse(upstream1.URL)
	parsed2, _ := url.Parse(upstream2.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed1, APIKeys: []string{"k1"}, KeyRowIDs: []int64{10}, Name: "u1"},
		{ID: 2, BaseURL: parsed2, APIKeys: []string{"k2"}, KeyRowIDs: []int64{20}, Name: "u2"},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	// The last upstream's response should be forwarded.
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

// ---------------------------------------------------------------------------
// Failover with network error on first upstream
// ---------------------------------------------------------------------------

func TestServeHTTP_Failover_NetworkError_TriesNext(t *testing.T) {
	// First upstream: connection refused.
	// Second upstream: healthy.
	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"from":"second"}`))
	}))
	defer upstream2.Close()

	dp := NewDynamicProxy()
	parsed1, _ := url.Parse("http://127.0.0.1:1") // connection refused
	parsed2, _ := url.Parse(upstream2.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed1, APIKeys: []string{"k1"}, KeyRowIDs: []int64{10}, Name: "dead"},
		{ID: 2, BaseURL: parsed2, APIKeys: []string{"k2"}, KeyRowIDs: []int64{20}, Name: "alive"},
	})

	var failedKeys []int64
	dp.KeyFailCallback = func(upstreamID, keyRowID int64) {
		failedKeys = append(failedKeys, keyRowID)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, failedKeys, int64(10), "failed key callback should fire for dead upstream")
}

// ---------------------------------------------------------------------------
// shouldFailoverStatus additional
// ---------------------------------------------------------------------------

func TestShouldFailoverStatus_Extended(t *testing.T) {
	// Verify 500 does NOT trigger failover.
	assert.False(t, shouldFailoverStatus(500))
	// Verify 504 DOES trigger failover.
	assert.True(t, shouldFailoverStatus(504))
	// Verify normal 4xx don't trigger failover.
	assert.False(t, shouldFailoverStatus(404))
	assert.False(t, shouldFailoverStatus(422))
}

// ---------------------------------------------------------------------------
// Prober: concurrent GetCurrentID safety
// ---------------------------------------------------------------------------

func TestProber_GetCurrentID_ConcurrentSafety(t *testing.T) {
	s := newTestStore(t)
	dp := NewDynamicProxy()
	prober := NewUpstreamProber(s, dp, time.Minute, 5*time.Second)

	// Run concurrent reads/probes.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			_ = prober.GetCurrentID()
		}
	}()
	for ctx.Err() == nil {
		prober.ProbeNow()
	}
	<-done
	// Test passes if no race detected (run with -race).
}
