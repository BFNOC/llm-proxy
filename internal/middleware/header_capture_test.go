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
