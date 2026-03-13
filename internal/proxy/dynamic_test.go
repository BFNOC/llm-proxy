package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDynamicProxy_NoActiveUpstream_Returns503(t *testing.T) {
	dp := NewDynamicProxy()

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body["error"], "no active upstream")
}

func TestDynamicProxy_ProxiesToUpstream(t *testing.T) {
	// Start a mock upstream that records the request and responds with 200.
	var capturedPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"object":"list"}`)) //nolint:errcheck
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()

	parsed, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	dp.SetActiveUpstream(parsed, "test-key")

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/v1/models", capturedPath, "upstream should receive the original path")
}

func TestDynamicProxy_StreamingResponseHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: hello\n\n")) //nolint:errcheck
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	dp.SetActiveUpstream(parsed, "test-key")

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "no", rec.Header().Get("X-Accel-Buffering"))
	assert.Empty(t, rec.Header().Get("Content-Length"),
		"Content-Length should be stripped for SSE responses")
}

func TestDynamicProxy_SetAndGetActiveUpstream(t *testing.T) {
	dp := NewDynamicProxy()

	// Initially nil.
	assert.Nil(t, dp.GetActiveUpstream())

	u1, err := url.Parse("https://api.openai.com")
	require.NoError(t, err)
	dp.SetActiveUpstream(u1, "key1")

	got := dp.GetActiveUpstream()
	require.NotNil(t, got)
	assert.Equal(t, u1, got.BaseURL)
	assert.Equal(t, "key1", got.APIKey)

	// Swap to a different upstream.
	u2, err := url.Parse("https://api.anthropic.com")
	require.NoError(t, err)
	dp.SetActiveUpstream(u2, "key2")

	got = dp.GetActiveUpstream()
	require.NotNil(t, got)
	assert.Equal(t, u2, got.BaseURL)
	assert.Equal(t, "key2", got.APIKey)
}

func TestDynamicProxy_UpstreamError_Returns502(t *testing.T) {
	dp := NewDynamicProxy()

	// Point at an address that refuses connections.
	u, err := url.Parse("http://127.0.0.1:1") // port 1 is always closed
	require.NoError(t, err)
	dp.SetActiveUpstream(u, "")

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body["error"], "bad gateway")
}
