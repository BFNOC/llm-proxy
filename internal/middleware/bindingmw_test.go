package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	encKey := []byte("01234567890123456789012345678901") // 32 bytes
	s, err := store.NewStore(filepath.Join(dir, "test.db"), encKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// withKeyID injects a downstream key ID into the request context (using the
// package-private ctxKeyDownstreamKeyID). Tests must be in package middleware.
func withKeyID(r *http.Request, keyID int64) *http.Request {
	ctx := context.WithValue(r.Context(), ctxKeyDownstreamKeyID, keyID)
	return r.WithContext(ctx)
}

// nextRecorder is a simple handler that records whether it was called and
// captures the allowed upstream IDs from context.
type nextRecorder struct {
	called     bool
	allowedIDs []int64
}

func (n *nextRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n.called = true
	n.allowedIDs = proxy.AllowedUpstreamIDsFromContext(r.Context())
	w.WriteHeader(http.StatusOK)
}

func TestBindingMW_NoKeyID_PassesThrough(t *testing.T) {
	s := newTestStore(t)
	bc := NewBindingCache(s)
	next := &nextRecorder{}
	mw := UpstreamBindingMiddleware(bc, nil)(next)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	assert.True(t, next.called, "next should be called when keyID is 0")
	assert.Nil(t, next.allowedIDs, "no allowed IDs should be set")
}

func TestBindingMW_KeyWithBindings_SetsContext(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("test-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("up1", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("up2", "https://b.example.com", []string{"key-b"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	err = s.SetKeyUpstreams(dk.ID, []int64{u1.ID, u2.ID})
	require.NoError(t, err)

	bc := NewBindingCache(s)
	next := &nextRecorder{}
	mw := UpstreamBindingMiddleware(bc, nil)(next)

	req := httptest.NewRequest(http.MethodGet, "/v1/chat", nil)
	req = withKeyID(req, dk.ID)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	assert.True(t, next.called)
	assert.ElementsMatch(t, []int64{u1.ID, u2.ID}, next.allowedIDs)
}

func TestBindingMW_KeyWithNoBindings_NoContextValue(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("unbound-key", 0)
	require.NoError(t, err)

	bc := NewBindingCache(s)
	next := &nextRecorder{}
	mw := UpstreamBindingMiddleware(bc, nil)(next)

	req := httptest.NewRequest(http.MethodGet, "/v1/chat", nil)
	req = withKeyID(req, dk.ID)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	assert.True(t, next.called, "next should be called when key has no bindings")
	assert.Nil(t, next.allowedIDs, "unbound key should not set allowed IDs")
}

func TestBindingCache_ReloadReflectsChanges(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("reload-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("up1", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	bc := NewBindingCache(s)
	// Initially no bindings
	assert.Nil(t, bc.GetKeyUpstreamIDs(dk.ID))

	// Add binding and reload
	err = s.SetKeyUpstreams(dk.ID, []int64{u1.ID})
	require.NoError(t, err)
	bc.Reload()

	assert.Equal(t, []int64{u1.ID}, bc.GetKeyUpstreamIDs(dk.ID))
}

func TestBindingCache_ReloadFailureKeepsOldSnapshot(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("keep-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("up1", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	err = s.SetKeyUpstreams(dk.ID, []int64{u1.ID})
	require.NoError(t, err)

	bc := NewBindingCache(s)
	assert.Equal(t, []int64{u1.ID}, bc.GetKeyUpstreamIDs(dk.ID))

	// Close DB to force reload failure
	_ = s.Close()
	bc.Reload() // should log error and keep old snapshot

	// Old data still available
	assert.Equal(t, []int64{u1.ID}, bc.GetKeyUpstreamIDs(dk.ID))
}
