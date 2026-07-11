package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeaderCapture_DisabledByDefault(t *testing.T) {
	c := NewHeaderCapture(5)
	assert.False(t, c.IsEnabled())
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.201 (external, cli)")
	c.Capture(req)
	_, items := c.Snapshot()
	assert.Empty(t, items)
}

func TestHeaderCapture_RecordsWhenEnabled(t *testing.T) {
	c := NewHeaderCapture(5)
	c.SetEnabled(true)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages?beta=true", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.201 (external, cli)")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20,claude-code-20250219")
	req.Header.Set("x-app", "cli")
	req.Header.Set("Authorization", "Bearer sk-ant-oat01-secret-token-value-here")
	req.Header.Set("x-api-key", "sk-downstream-secret")
	c.Capture(req)

	enabled, items := c.Snapshot()
	require.True(t, enabled)
	require.Len(t, items, 1)
	assert.Equal(t, "/v1/messages", items[0].Path)
	assert.Equal(t, "claude-cli/2.1.201 (external, cli)", items[0].Flat["User-Agent"])
	assert.Equal(t, "cli", items[0].Flat["X-App"])
	assert.Contains(t, items[0].Flat["Authorization"], "Bearer ")
	assert.Contains(t, items[0].Flat["Authorization"], "…")
	assert.NotContains(t, items[0].Flat["Authorization"], "secret-token")
	assert.Contains(t, items[0].Flat["X-Api-Key"], "…")
}

func TestHeaderCapture_MiddlewarePassesThrough(t *testing.T) {
	c := NewHeaderCapture(5)
	c.SetEnabled(true)
	called := false
	h := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("anthropic-version", "2023-06-01")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assert.True(t, called)
	assert.Equal(t, http.StatusNoContent, rr.Code)
	_, items := c.Snapshot()
	require.Len(t, items, 1)
	assert.Equal(t, "2023-06-01", items[0].Flat["Anthropic-Version"])
}
