package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var auditMWTestKey = []byte("01234567890123456789012345678901")

func newAuditMWTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "auditmw_test.db")
	s, err := store.NewStore(dbPath, auditMWTestKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// ---------------------------------------------------------------------------
// AuditLogMiddleware integration tests
// ---------------------------------------------------------------------------

func TestAuditLogMiddleware_WritesLogEntry(t *testing.T) {
	s := newAuditMWTestStore(t)
	_, dk, err := s.CreateKey("audit-mw-key", 0)
	require.NoError(t, err)

	al := NewAuditLogger(s, nil, 100, 1, 50*time.Millisecond)
	defer al.Stop()

	// The inner handler simulates a successful proxy response.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream-Name", "test-upstream")
		w.Header().Set("X-API-Key-Index", "2")
		w.Header().Set("X-Model", "gpt-4o")
		w.Header().Set("X-Used-Proxy", "socks5://proxy.local")
		w.WriteHeader(http.StatusOK)
	})

	mw := AuditLogMiddleware(al)(inner)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("X-Real-IP", "203.0.113.42")
	// Inject downstream key ID into context (simulating KeyResolver having run).
	ctx := context.WithValue(req.Context(), ctxKeyDownstreamKeyID, dk.ID)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Wait for the ticker flush.
	time.Sleep(200 * time.Millisecond)

	now := time.Now().UTC()
	logs, err := s.QueryLogs(dk.ID, now.Add(-time.Minute), now.Add(time.Minute), 100)
	require.NoError(t, err)
	require.Len(t, logs, 1, "one log entry should be written")

	assert.Equal(t, "test-upstream", logs[0].UpstreamName)
	assert.Equal(t, 2, logs[0].UpstreamKeyIdx)
	assert.Equal(t, "gpt-4o", logs[0].Model)
	assert.Equal(t, "socks5://proxy.local", logs[0].UsedProxy)
	assert.Equal(t, "203.0.113.42", logs[0].ClientIP)
	assert.Equal(t, "/v1/chat/completions", logs[0].Path)
	assert.Equal(t, 200, logs[0].StatusCode)
}

func TestAuditLogMiddleware_InternalHeadersRemovedFromResponse(t *testing.T) {
	s := newAuditMWTestStore(t)

	al := NewAuditLogger(s, nil, 100, 100, time.Hour)
	defer al.Stop()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream-Name", "secret-upstream")
		w.Header().Set("X-API-Key-Index", "0")
		w.Header().Set("X-Model", "claude-opus-4-20250514")
		w.Header().Set("X-Used-Proxy", "http://proxy.internal")
		w.WriteHeader(http.StatusOK)
	})

	mw := AuditLogMiddleware(al)(inner)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	// Internal headers must NOT leak to the client.
	assert.Empty(t, rec.Header().Get("X-Upstream-Name"), "X-Upstream-Name should be removed")
	assert.Empty(t, rec.Header().Get("X-API-Key-Index"), "X-API-Key-Index should be removed")
	assert.Empty(t, rec.Header().Get("X-Model"), "X-Model should be removed")
	assert.Empty(t, rec.Header().Get("X-Used-Proxy"), "X-Used-Proxy should be removed")
}

func TestAuditLogMiddleware_ClientIP_XForwardedFor(t *testing.T) {
	s := newAuditMWTestStore(t)
	_, dk, err := s.CreateKey("xff-key", 0)
	require.NoError(t, err)

	al := NewAuditLogger(s, nil, 100, 1, 50*time.Millisecond)
	defer al.Stop()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := AuditLogMiddleware(al)(inner)

	req := httptest.NewRequest(http.MethodPost, "/v1/completions", nil)
	req.Header.Set("X-Forwarded-For", "198.51.100.1, 10.0.0.1")
	ctx := context.WithValue(req.Context(), ctxKeyDownstreamKeyID, dk.ID)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	time.Sleep(200 * time.Millisecond)

	now := time.Now().UTC()
	logs, err := s.QueryLogs(dk.ID, now.Add(-time.Minute), now.Add(time.Minute), 100)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "198.51.100.1", logs[0].ClientIP, "should take first IP from X-Forwarded-For")
}

// ---------------------------------------------------------------------------
// responseStatusCapture — Hijack
// ---------------------------------------------------------------------------

func TestResponseStatusCapture_Hijack_NotSupported(t *testing.T) {
	// httptest.ResponseRecorder does not implement http.Hijacker.
	rec := httptest.NewRecorder()
	capture := &responseStatusCapture{ResponseWriter: rec, statusCode: http.StatusOK}

	conn, rw, err := capture.Hijack()
	assert.Nil(t, conn)
	assert.Nil(t, rw)
	assert.Error(t, err, "should return error when underlying writer does not support Hijack")
}

func TestResponseStatusCapture_Flush_PassesThrough(t *testing.T) {
	rec := httptest.NewRecorder()
	capture := &responseStatusCapture{ResponseWriter: rec, statusCode: http.StatusOK}

	// httptest.ResponseRecorder implements http.Flusher; should not panic.
	capture.Flush()
	assert.True(t, rec.Flushed)
}
