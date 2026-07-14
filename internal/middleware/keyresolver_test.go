package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var resolverTestKey = []byte("01234567890123456789012345678901")

func newResolverTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "resolver_test.db")
	s, err := store.NewStore(dbPath, resolverTestKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// ---------------------------------------------------------------------------
// KeyCache / KeySnapshot unit tests
// ---------------------------------------------------------------------------

func TestNewKeyCache_EmptySnapshot(t *testing.T) {
	kc := NewKeyCache()
	snap := kc.get()
	assert.NotNil(t, snap)
	assert.Nil(t, snap.Lookup("nonexistent"))
}

func TestKeyCache_Reload(t *testing.T) {
	s := newResolverTestStore(t)

	plaintext1, dk1, err := s.CreateKey("key-one", 100)
	require.NoError(t, err)
	hash1 := proxy.HashKey(plaintext1)

	plaintext2, dk2, err := s.CreateKey("key-two", 200)
	require.NoError(t, err)
	hash2 := proxy.HashKey(plaintext2)

	kc := NewKeyCache()

	// Before reload: empty snapshot
	assert.Nil(t, kc.get().Lookup(hash1))

	// Reload from store
	err = kc.Reload(s)
	require.NoError(t, err)

	// After reload: both keys visible
	r1 := kc.get().Lookup(hash1)
	require.NotNil(t, r1)
	assert.Equal(t, dk1.ID, r1.ID)
	assert.Equal(t, "key-one", r1.Name)
	assert.Equal(t, 100, r1.RPMLimit)
	assert.True(t, r1.Enabled)

	r2 := kc.get().Lookup(hash2)
	require.NotNil(t, r2)
	assert.Equal(t, dk2.ID, r2.ID)
	assert.Equal(t, "key-two", r2.Name)
	assert.Equal(t, 200, r2.RPMLimit)
}

func TestKeyCache_Reload_ReflectsDisabledKey(t *testing.T) {
	s := newResolverTestStore(t)

	plaintext, dk, err := s.CreateKey("disable-me", 0)
	require.NoError(t, err)
	hash := proxy.HashKey(plaintext)

	kc := NewKeyCache()
	require.NoError(t, kc.Reload(s))

	r := kc.get().Lookup(hash)
	require.NotNil(t, r)
	assert.True(t, r.Enabled)

	// Disable the key in the store
	_, err = s.UpdateKey(dk.ID, "disable-me", 0, false)
	require.NoError(t, err)

	// Reload picks up the change
	require.NoError(t, kc.Reload(s))

	r = kc.get().Lookup(hash)
	require.NotNil(t, r)
	assert.False(t, r.Enabled)
}

func TestKeyCache_Reload_ErrorKeepsOldSnapshot(t *testing.T) {
	s := newResolverTestStore(t)

	plaintext, _, err := s.CreateKey("survive-key", 50)
	require.NoError(t, err)
	hash := proxy.HashKey(plaintext)

	kc := NewKeyCache()
	require.NoError(t, kc.Reload(s))
	require.NotNil(t, kc.get().Lookup(hash), "key should be loaded")

	// Close the DB to make the next Reload fail.
	_ = s.Close()

	err = kc.Reload(s)
	assert.Error(t, err, "Reload should return an error on closed store")

	// Old snapshot must survive the failed reload.
	r := kc.get().Lookup(hash)
	require.NotNil(t, r, "old snapshot should be retained after failed reload")
	assert.Equal(t, "survive-key", r.Name)
	assert.Equal(t, 50, r.RPMLimit)
}

func TestKeySnapshot_Lookup_NilSnapshot(t *testing.T) {
	var snap *KeySnapshot
	assert.Nil(t, snap.Lookup("anything"))
}

// ---------------------------------------------------------------------------
// KeyResolverMiddleware tests
// ---------------------------------------------------------------------------

// ctxWithHash injects a key hash into the request context, simulating the
// classifier middleware having run first.
func ctxWithHash(r *http.Request, hash string) *http.Request {
	ctx := context.WithValue(r.Context(), ctxKeyHash, hash)
	return r.WithContext(ctx)
}

func TestKeyResolverMiddleware_ValidKey(t *testing.T) {
	s := newResolverTestStore(t)
	plaintext, dk, err := s.CreateKey("valid-key", 60)
	require.NoError(t, err)
	hash := proxy.HashKey(plaintext)

	kc := NewKeyCache()
	require.NoError(t, kc.Reload(s))

	var capturedResolvedKey *store.DownstreamKey
	var capturedKeyID int64
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedResolvedKey = ResolvedKeyFromContext(r.Context())
		capturedKeyID = DownstreamKeyIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mw := KeyResolverMiddleware(kc)(next)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = ctxWithHash(req, hash)
	rec := httptest.NewRecorder()

	mw.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, capturedResolvedKey)
	assert.Equal(t, dk.ID, capturedResolvedKey.ID)
	assert.Equal(t, hash, capturedResolvedKey.KeyHash)
	assert.Equal(t, "valid-key", capturedResolvedKey.Name)
	assert.Equal(t, 60, capturedResolvedKey.RPMLimit)
	assert.True(t, capturedResolvedKey.Enabled)
	assert.Equal(t, dk.ID, capturedKeyID)
}

func TestKeyResolverMiddleware_InvalidKey(t *testing.T) {
	kc := NewKeyCache() // empty cache, no keys loaded

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	mw := KeyResolverMiddleware(kc)(next)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = ctxWithHash(req, "nonexistent-hash-value")
	rec := httptest.NewRecorder()

	mw.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.False(t, nextCalled)

	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body["error"], "invalid or disabled API key")
}

func TestKeyResolverMiddleware_DisabledKey(t *testing.T) {
	s := newResolverTestStore(t)
	plaintext, dk, err := s.CreateKey("disabled-key", 0)
	require.NoError(t, err)
	hash := proxy.HashKey(plaintext)

	// Disable the key
	_, err = s.UpdateKey(dk.ID, "disabled-key", 0, false)
	require.NoError(t, err)

	kc := NewKeyCache()
	require.NoError(t, kc.Reload(s))

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	mw := KeyResolverMiddleware(kc)(next)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = ctxWithHash(req, hash)
	rec := httptest.NewRecorder()

	mw.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.False(t, nextCalled)

	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body["error"], "invalid or disabled API key")
}

func TestKeyResolverMiddleware_MissingHash(t *testing.T) {
	kc := NewKeyCache()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	mw := KeyResolverMiddleware(kc)(next)

	// Request with no hash in context (simulates classifier not having run)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()

	mw.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.False(t, nextCalled)
}

func TestKeyResolverMiddleware_ResponseContentType(t *testing.T) {
	kc := NewKeyCache()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	mw := KeyResolverMiddleware(kc)(next)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = ctxWithHash(req, "unknown-hash")
	rec := httptest.NewRecorder()

	mw.ServeHTTP(rec, req)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}
