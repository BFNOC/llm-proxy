package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeaderCapture_DisabledByDefault(t *testing.T) {
	c := NewHeaderCapture(5)
	assert.False(t, c.IsEnabled())
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"a":1}`))
	req.Header.Set("User-Agent", "claude-cli/2.1.201 (external, cli)")
	c.Capture(req)
	_, items := c.Snapshot()
	assert.Empty(t, items)
}

func TestHeaderCapture_RecordsFullSecretsAndBody(t *testing.T) {
	c := NewHeaderCapture(5)
	c.SetEnabled(true)
	body := `{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages?beta=true", strings.NewReader(body))
	req.Header.Set("User-Agent", "claude-cli/2.1.201 (external, cli)")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20,claude-code-20250219")
	req.Header.Set("x-app", "cli")
	req.Header.Set("Authorization", "Bearer sk-ant-oat01-secret-token-value-here")
	req.Header.Set("x-api-key", "sk-downstream-secret")
	req = c.Capture(req)

	enabled, items := c.Snapshot()
	require.True(t, enabled)
	require.Len(t, items, 1)
	assert.Equal(t, "/v1/messages", items[0].Path)
	assert.Equal(t, "claude-cli/2.1.201 (external, cli)", items[0].Flat["User-Agent"])
	assert.Equal(t, "cli", items[0].Flat["X-App"])
	// Full secrets preserved (admin-only debug feature).
	assert.Equal(t, "Bearer sk-ant-oat01-secret-token-value-here", items[0].Flat["Authorization"])
	assert.Equal(t, "sk-downstream-secret", items[0].Flat["X-Api-Key"])
	assert.Equal(t, body, items[0].Body)
	assert.False(t, items[0].BodyTruncated)

	// Downstream can still read the body.
	got, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	assert.Equal(t, body, string(got))
}

func TestHeaderCapture_BodyTruncated(t *testing.T) {
	c := NewHeaderCapture(5)
	c.bodyMax = 10
	c.SetEnabled(true)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("0123456789ABCDEF"))
	req = c.Capture(req)
	_, items := c.Snapshot()
	require.Len(t, items, 1)
	assert.True(t, items[0].BodyTruncated)
	assert.Equal(t, 10, items[0].BodyBytes)
	assert.Equal(t, "0123456789", items[0].Body)
}

func TestHeaderCapture_MiddlewarePassesThrough(t *testing.T) {
	c := NewHeaderCapture(5)
	c.SetEnabled(true)
	called := false
	var seenBody string
	h := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		b, _ := io.ReadAll(r.Body)
		seenBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/models", strings.NewReader(`{"ok":true}`))
	req.Header.Set("anthropic-version", "2023-06-01")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assert.True(t, called)
	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.Equal(t, `{"ok":true}`, seenBody)
	_, items := c.Snapshot()
	require.Len(t, items, 1)
	assert.Equal(t, "2023-06-01", items[0].Flat["Anthropic-Version"])
	assert.Equal(t, `{"ok":true}`, items[0].Body)
}

// ---------------------------------------------------------------------------
// Clear
// ---------------------------------------------------------------------------

func TestHeaderCapture_Clear(t *testing.T) {
	c := NewHeaderCapture(5)
	c.SetEnabled(true)

	// Capture a request.
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"a":1}`))
	c.Capture(req)
	_, items := c.Snapshot()
	require.Len(t, items, 1, "should have 1 capture before clear")

	// Clear and verify empty.
	c.Clear()
	_, items = c.Snapshot()
	assert.Empty(t, items, "should be empty after clear")
}

func TestHeaderCapture_Clear_NilSafe(t *testing.T) {
	var c *HeaderCapture
	c.Clear() // should not panic
}

// ---------------------------------------------------------------------------
// Latest
// ---------------------------------------------------------------------------

func TestHeaderCapture_Latest_ReturnsNewest(t *testing.T) {
	c := NewHeaderCapture(5)
	c.SetEnabled(true)

	req1 := httptest.NewRequest(http.MethodPost, "/v1/first", strings.NewReader(`{"n":1}`))
	c.Capture(req1)
	req2 := httptest.NewRequest(http.MethodPost, "/v1/second", strings.NewReader(`{"n":2}`))
	c.Capture(req2)

	latest, ok := c.Latest()
	require.True(t, ok)
	assert.Equal(t, "/v1/second", latest.Path, "Latest should return the most recent capture")
}

func TestHeaderCapture_Latest_Empty(t *testing.T) {
	c := NewHeaderCapture(5)
	_, ok := c.Latest()
	assert.False(t, ok, "Latest on empty capture should return false")
}

func TestHeaderCapture_Latest_NilSafe(t *testing.T) {
	var c *HeaderCapture
	_, ok := c.Latest()
	assert.False(t, ok, "Latest on nil capture should return false")
}

// ---------------------------------------------------------------------------
// Middleware integration (full round-trip with custom headers)
// ---------------------------------------------------------------------------

func TestHeaderCapture_Middleware_Integration(t *testing.T) {
	c := NewHeaderCapture(10)
	c.SetEnabled(true)

	var downstreamBody string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		downstreamBody = string(b)
		w.WriteHeader(http.StatusCreated)
	})

	handler := c.Middleware(inner)

	body := `{"model":"claude-sonnet-4-20250514","prompt":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/completions?stream=true", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer sk-test-key-123")
	req.Header.Set("X-Custom-Header", "custom-value")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Downstream handler received the body.
	assert.Equal(t, body, downstreamBody)
	assert.Equal(t, http.StatusCreated, rr.Code)

	// Capture recorded everything.
	latest, ok := c.Latest()
	require.True(t, ok)
	assert.Equal(t, "/v1/completions", latest.Path)
	assert.Equal(t, "stream=true", latest.Query)
	assert.Equal(t, "POST", latest.Method)
	assert.Equal(t, "Bearer sk-test-key-123", latest.Flat["Authorization"])
	assert.Equal(t, "custom-value", latest.Flat["X-Custom-Header"])
	assert.Equal(t, body, latest.Body)
	assert.False(t, latest.BodyTruncated)
}

func TestHeaderCapture_Middleware_DisabledDoesNotCapture(t *testing.T) {
	c := NewHeaderCapture(5)
	// Not enabled.

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := c.Middleware(inner)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	_, items := c.Snapshot()
	assert.Empty(t, items, "disabled capture should not record")
}
