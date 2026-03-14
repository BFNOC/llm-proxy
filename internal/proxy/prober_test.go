package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testEncryptionKey is a deterministic 32-byte key used across all prober tests.
var testEncryptionKey = []byte("test-key-must-be-32-bytes-long!!")

// newTestStore creates a temporary SQLite-backed store for the duration of t.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.NewStore(filepath.Join(t.TempDir(), "test.db"), testEncryptionKey)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

// healthyServer returns an httptest.Server that responds 200 to all requests.
func healthyServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// unhealthyServer returns an httptest.Server that responds 500 to all requests.
func unhealthyServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// authRequiredServer returns a server that responds 401 — still "reachable".
func authRequiredServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProber_SelectsHealthyUpstream(t *testing.T) {
	s := newTestStore(t)
	dp := NewDynamicProxy()

	healthy := healthyServer(t)
	_, err := s.CreateUpstream("primary", healthy.URL, "key-primary", 1)
	require.NoError(t, err)

	prober := NewUpstreamProber(s, dp, time.Minute, 5*time.Second)
	prober.probeOnce()

	all := dp.GetAllUpstreams()
	require.Len(t, all, 1)
	assert.Equal(t, "key-primary", all[0].APIKey)
	assert.Equal(t, "primary", all[0].Name)
}

func TestProber_SkipsUnhealthyUpstreams(t *testing.T) {
	s := newTestStore(t)
	dp := NewDynamicProxy()

	bad := unhealthyServer(t)
	good := healthyServer(t)

	_, err := s.CreateUpstream("bad", bad.URL, "key-bad", 0) // priority 0 = higher preference
	require.NoError(t, err)
	_, err = s.CreateUpstream("good", good.URL, "key-good", 1)
	require.NoError(t, err)

	prober := NewUpstreamProber(s, dp, time.Minute, 5*time.Second)
	prober.probeOnce()

	// Only the healthy upstream should be in the list.
	all := dp.GetAllUpstreams()
	require.Len(t, all, 1)
	assert.Equal(t, "key-good", all[0].APIKey, "should skip unhealthy upstream")
}

func TestProber_BothHealthy_BothInList(t *testing.T) {
	s := newTestStore(t)
	dp := NewDynamicProxy()

	srv1 := healthyServer(t)
	srv2 := healthyServer(t)

	_, err := s.CreateUpstream("first", srv1.URL, "key-1", 0)
	require.NoError(t, err)
	_, err = s.CreateUpstream("second", srv2.URL, "key-2", 1)
	require.NoError(t, err)

	prober := NewUpstreamProber(s, dp, time.Minute, 5*time.Second)
	prober.probeOnce()

	// Both healthy upstreams should be in the list, sorted by priority.
	all := dp.GetAllUpstreams()
	require.Len(t, all, 2)
	assert.Equal(t, "key-1", all[0].APIKey, "priority 0 should be first")
	assert.Equal(t, "key-2", all[1].APIKey, "priority 1 should be second")
}

func TestProber_SwitchesOnCurrentFailure(t *testing.T) {
	s := newTestStore(t)
	dp := NewDynamicProxy()

	bad := unhealthyServer(t)
	good := healthyServer(t)

	_, err := s.CreateUpstream("primary", bad.URL, "key-bad", 0)
	require.NoError(t, err)
	_, err = s.CreateUpstream("fallback", good.URL, "key-good", 1)
	require.NoError(t, err)

	prober := NewUpstreamProber(s, dp, time.Minute, 5*time.Second)
	prober.probeOnce()

	// Only fallback should be in the list since primary is unhealthy.
	all := dp.GetAllUpstreams()
	require.Len(t, all, 1)
	assert.Equal(t, "key-good", all[0].APIKey)
}

func TestProber_AllUnhealthyKeepsLastActive(t *testing.T) {
	s := newTestStore(t)
	dp := NewDynamicProxy()

	bad1 := unhealthyServer(t)
	bad2 := unhealthyServer(t)

	_, err := s.CreateUpstream("bad1", bad1.URL, "key-1", 0)
	require.NoError(t, err)
	_, err = s.CreateUpstream("bad2", bad2.URL, "key-2", 1)
	require.NoError(t, err)

	prober := NewUpstreamProber(s, dp, time.Minute, 5*time.Second)

	// Seed an existing upstream list as if they were previously healthy.
	dp.SetActiveUpstream(mustParse(t, bad1.URL), "key-1", "bad1")

	prober.probeOnce()

	// Must keep the last active list to avoid 503 storm.
	require.NotNil(t, dp.GetActiveUpstream())
	assert.Equal(t, "key-1", dp.GetActiveUpstream().APIKey)
}

func TestProber_AcceptsAuthErrorAsReachable(t *testing.T) {
	s := newTestStore(t)
	dp := NewDynamicProxy()

	// 401 = server is up, just needs valid credentials.
	srv := authRequiredServer(t)
	_, err := s.CreateUpstream("auth-required", srv.URL, "key-auth", 0)
	require.NoError(t, err)

	prober := NewUpstreamProber(s, dp, time.Minute, 5*time.Second)
	prober.probeOnce()

	all := dp.GetAllUpstreams()
	require.Len(t, all, 1)
	assert.Equal(t, "key-auth", all[0].APIKey)
}

func TestProber_NoUpstreams(t *testing.T) {
	s := newTestStore(t)
	dp := NewDynamicProxy()

	prober := NewUpstreamProber(s, dp, time.Minute, 5*time.Second)
	// Must not panic or error when the store is empty.
	prober.probeOnce()

	assert.Nil(t, dp.GetActiveUpstream())
	assert.Empty(t, dp.GetAllUpstreams())
}

func TestProber_StartContextCancellation(t *testing.T) {
	s := newTestStore(t)
	dp := NewDynamicProxy()

	healthy := healthyServer(t)
	_, err := s.CreateUpstream("primary", healthy.URL, "key-1", 0)
	require.NoError(t, err)

	prober := NewUpstreamProber(s, dp, 50*time.Millisecond, 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		prober.Start(ctx)
	}()

	// Wait for at least one successful probe.
	assert.Eventually(t, func() bool {
		return dp.GetActiveUpstream() != nil
	}, 2*time.Second, 20*time.Millisecond)

	cancel()

	select {
	case <-done:
		// Start exited after cancellation — expected.
	case <-time.After(2 * time.Second):
		t.Fatal("prober.Start did not return after context cancellation")
	}
}

// mustParse is a test helper that parses a URL or fails the test immediately.
func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}
