package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
	next := &nextRecorder{}
	mw := UpstreamBindingMiddleware(s)(next)

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
	u1, err := s.CreateUpstream("up1", "https://a.example.com", "key-a", 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("up2", "https://b.example.com", "key-b", 0)
	require.NoError(t, err)
	err = s.SetKeyUpstreams(dk.ID, []int64{u1.ID, u2.ID})
	require.NoError(t, err)

	next := &nextRecorder{}
	mw := UpstreamBindingMiddleware(s)(next)

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

	next := &nextRecorder{}
	mw := UpstreamBindingMiddleware(s)(next)

	req := httptest.NewRequest(http.MethodGet, "/v1/chat", nil)
	req = withKeyID(req, dk.ID)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	assert.True(t, next.called, "next should be called when key has no bindings")
	assert.Nil(t, next.allowedIDs, "unbound key should not set allowed IDs")
}

func TestBindingMW_StoreError_Returns503(t *testing.T) {
	// Create a store, then close it to force query errors
	dir := t.TempDir()
	encKey := []byte("01234567890123456789012345678901")
	s, err := store.NewStore(filepath.Join(dir, "test.db"), encKey)
	require.NoError(t, err)

	// Create a key first while store is open
	_, dk, err := s.CreateKey("error-key", 0)
	require.NoError(t, err)

	// Close the store to force DB errors
	_ = s.Close()
	// Remove the database file
	_ = os.Remove(filepath.Join(dir, "test.db"))

	next := &nextRecorder{}
	mw := UpstreamBindingMiddleware(s)(next)

	req := httptest.NewRequest(http.MethodGet, "/v1/chat", nil)
	req = withKeyID(req, dk.ID)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	assert.False(t, next.called, "next should NOT be called on store error (fail-closed)")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body["error"], "upstream binding lookup failed")
}
