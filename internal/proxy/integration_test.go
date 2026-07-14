package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
// DynamicProxy.ServeHTTP integration tests
// ---------------------------------------------------------------------------

func TestServeHTTP_FullProxyFlow(t *testing.T) {
	// Mock upstream serving /v1/chat/completions with a JSON response.
	var capturedPath, capturedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-123",
			"object":  "chat.completion",
			"model":   "gpt-4",
			"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "Hello!"}}},
		})
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	dp.SetAllUpstreams([]*ActiveUpstream{{
		ID:                1,
		BaseURL:           parsed,
		APIKeys:           []string{"upstream-key-1"},
		KeyRowIDs:         []int64{100},
		Name:              "test-upstream",
		KeySchedulingMode: "round-robin",
	}})

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-downstream-key")
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/v1/chat/completions", capturedPath)
	assert.Equal(t, "Bearer upstream-key-1", capturedAuth, "auth header should be rewritten to upstream key")

	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "chatcmpl-123", resp["id"])
}

func TestServeHTTP_BodyTooLarge_Returns413(t *testing.T) {
	dp := NewDynamicProxy()
	parsed, err := url.Parse("http://127.0.0.1:9999")
	require.NoError(t, err)
	dp.SetAllUpstreams([]*ActiveUpstream{{
		ID:      1,
		BaseURL: parsed,
		APIKeys: []string{"key"},
		Name:    "dummy",
	}})

	// Create a body larger than 32MB.
	bigBody := make([]byte, 32<<20+1)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(bigBody))
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body["error"], "request body too large")
}

func TestServeHTTP_NoActiveUpstreams_Returns503(t *testing.T) {
	dp := NewDynamicProxy()
	// No upstreams set.
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body["error"], "no active upstream")
}

func TestServeHTTP_WhitelistRejection_Returns403(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be reached when model is rejected by whitelist")
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	dp.SetAllUpstreams([]*ActiveUpstream{{
		ID:      1,
		BaseURL: parsed,
		APIKeys: []string{"key"},
		Name:    "test",
	}})
	dp.WhitelistMatcher = func(model string) bool {
		return model == "gpt-4" // only gpt-4 allowed
	}

	body := `{"model":"gpt-3.5-turbo","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	errObj := resp["error"].(map[string]interface{})
	assert.Equal(t, "model_not_allowed", errObj["code"])
	assert.Contains(t, errObj["message"], "gpt-3.5-turbo")
}

func TestServeHTTP_WhitelistAllowed_Proxies(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	dp.SetAllUpstreams([]*ActiveUpstream{{
		ID:      1,
		BaseURL: parsed,
		APIKeys: []string{"key"},
		Name:    "test",
	}})
	dp.WhitelistMatcher = func(model string) bool {
		return model == "gpt-4"
	}

	body := `{"model":"gpt-4","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestServeHTTP_AllowedUpstreamIDs_Filtering(t *testing.T) {
	// Two upstreams but only one is allowed for this key.
	var hitUpstreamName string
	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitUpstreamName = "upstream1"
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"source":"upstream1"}`))
	}))
	defer upstream1.Close()

	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitUpstreamName = "upstream2"
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"source":"upstream2"}`))
	}))
	defer upstream2.Close()

	dp := NewDynamicProxy()
	parsed1, _ := url.Parse(upstream1.URL)
	parsed2, _ := url.Parse(upstream2.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 10, BaseURL: parsed1, APIKeys: []string{"k1"}, KeyRowIDs: []int64{1}, Name: "upstream1"},
		{ID: 20, BaseURL: parsed2, APIKeys: []string{"k2"}, KeyRowIDs: []int64{2}, Name: "upstream2"},
	})

	// Only allow upstream ID 20.
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx := ContextWithAllowedUpstreamIDs(req.Context(), []int64{20})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "upstream2", hitUpstreamName, "should only route to allowed upstream")
}

func TestServeHTTP_AllowedUpstreamIDs_NoneAvailable_Returns403(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be reached")
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 10, BaseURL: parsed, APIKeys: []string{"k1"}, Name: "upstream1"},
	})

	// Allow only upstream ID 99, which doesn't exist.
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx := ContextWithAllowedUpstreamIDs(req.Context(), []int64{99})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body["error"], "no permitted upstream")
}

func TestServeHTTP_ModelPatternFiltering_422(t *testing.T) {
	// Upstream only accepts claude-* models.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be reached for non-matching model")
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed, APIKeys: []string{"k1"}, Name: "claude-only", ModelPatterns: []string{"claude-*"}},
	})

	body := `{"model":"gpt-4","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	errObj := resp["error"].(map[string]interface{})
	assert.Equal(t, "model_not_available", errObj["code"])
}

func TestServeHTTP_PerKeyModelOverride_ForcesUpstream(t *testing.T) {
	var hitName string
	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitName = "default"
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer upstream1.Close()
	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitName = "override"
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer upstream2.Close()

	dp := NewDynamicProxy()
	parsed1, _ := url.Parse(upstream1.URL)
	parsed2, _ := url.Parse(upstream2.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed1, APIKeys: []string{"k1"}, KeyRowIDs: []int64{10}, Name: "default"},
		{ID: 2, BaseURL: parsed2, APIKeys: []string{"k2"}, KeyRowIDs: []int64{20}, Name: "override"},
	})

	// Set per-key override: gpt-4 -> upstream 2.
	body := `{"model":"gpt-4","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := ContextWithKeyModelOverrides(req.Context(), []KeyModelOverrideRule{
		{ModelPattern: "gpt-4", UpstreamIDs: []int64{2}},
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "override", hitName, "per-key override should force routing to upstream 2")
}

func TestServeHTTP_PerKeyModelOverride_NoUpstreamAvailable_422(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed, APIKeys: []string{"k1"}, Name: "only"},
	})

	// Override points to upstream 99, which doesn't exist.
	body := `{"model":"gpt-4","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := ContextWithKeyModelOverrides(req.Context(), []KeyModelOverrideRule{
		{ModelPattern: "gpt-4", UpstreamIDs: []int64{99}},
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	errObj := resp["error"].(map[string]interface{})
	assert.Equal(t, "override_upstream_unavailable", errObj["code"])
}

func TestServeHTTP_Failover_429_TriesNextUpstream(t *testing.T) {
	callCount := 0
	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limit"}}`))
	}))
	defer upstream1.Close()

	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"from-second"}`))
	}))
	defer upstream2.Close()

	dp := NewDynamicProxy()
	parsed1, _ := url.Parse(upstream1.URL)
	parsed2, _ := url.Parse(upstream2.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed1, APIKeys: []string{"k1"}, KeyRowIDs: []int64{10}, Name: "rate-limited"},
		{ID: 2, BaseURL: parsed2, APIKeys: []string{"k2"}, KeyRowIDs: []int64{20}, Name: "ok"},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 2, callCount, "should try first upstream (429) then succeed on second")
}

func TestServeHTTP_Failover_401_TriesNextUpstream(t *testing.T) {
	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer upstream1.Close()

	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream2.Close()

	dp := NewDynamicProxy()
	parsed1, _ := url.Parse(upstream1.URL)
	parsed2, _ := url.Parse(upstream2.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed1, APIKeys: []string{"k1"}, KeyRowIDs: []int64{10}, Name: "bad-auth"},
		{ID: 2, BaseURL: parsed2, APIKeys: []string{"k2"}, KeyRowIDs: []int64{20}, Name: "good"},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestServeHTTP_KeyCallbacks(t *testing.T) {
	var failedUpstream int64
	var successUpstream, successKey int64

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 5, BaseURL: parsed, APIKeys: []string{"k1"}, KeyRowIDs: []int64{50}, Name: "test"},
	})
	dp.KeySuccessCallback = func(upstreamID, keyRowID int64) {
		successUpstream = upstreamID
		successKey = keyRowID
	}
	dp.KeyFailCallback = func(upstreamID, keyRowID int64) {
		failedUpstream = upstreamID
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int64(5), successUpstream)
	assert.Equal(t, int64(50), successKey)
	assert.Equal(t, int64(0), failedUpstream, "no failure expected")
}

func TestServeHTTP_KeyFailCallback_OnFailover(t *testing.T) {
	var failures []int64

	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limit"}}`))
	}))
	defer upstream1.Close()

	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer upstream2.Close()

	dp := NewDynamicProxy()
	parsed1, _ := url.Parse(upstream1.URL)
	parsed2, _ := url.Parse(upstream2.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed1, APIKeys: []string{"k1"}, KeyRowIDs: []int64{10}, Name: "rate-limited"},
		{ID: 2, BaseURL: parsed2, APIKeys: []string{"k2"}, KeyRowIDs: []int64{20}, Name: "ok"},
	})
	dp.KeyFailCallback = func(upstreamID, keyRowID int64) {
		failures = append(failures, keyRowID)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, failures, int64(10), "key 10 should have fail callback")
}

func TestServeHTTP_ActiveRequestsCounter(t *testing.T) {
	dp := NewDynamicProxy()
	assert.Equal(t, int64(0), dp.ActiveRequests())

	// Use a channel to hold the upstream handler until we check the counter.
	hold := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	// Wait until the active counter increments.
	assert.Eventually(t, func() bool {
		return dp.ActiveRequests() > 0
	}, 2*time.Second, 10*time.Millisecond, "active requests should increment")

	close(hold)
	<-done
	assert.Equal(t, int64(0), dp.ActiveRequests(), "active requests should decrement after completion")
}

func TestServeHTTP_UpstreamPathPrefix(t *testing.T) {
	var capturedPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL + "/prefix")
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed, APIKeys: []string{"k"}, Name: "prefixed"},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/prefix/v1/models", capturedPath, "path should include upstream prefix")
}

func TestServeHTTP_StripsUntrustedHeaders(t *testing.T) {
	var captured http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed, APIKeys: []string{"k"}, Name: "test"},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "5.6.7.8")
	req.Header.Set("CF-Connecting-IP", "9.10.11.12")
	req.Header.Set("True-Client-IP", "13.14.15.16")
	req.Header.Set("X-Custom-Header", "keep-me")
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, captured.Get("X-Forwarded-For"), "untrusted header should be stripped")
	assert.Empty(t, captured.Get("X-Real-IP"), "untrusted header should be stripped")
	assert.Empty(t, captured.Get("CF-Connecting-IP"), "untrusted header should be stripped")
	assert.Empty(t, captured.Get("True-Client-IP"), "untrusted header should be stripped")
	assert.Equal(t, "keep-me", captured.Get("X-Custom-Header"), "non-untrusted header should be preserved")
}

func TestServeHTTP_StripsSensitiveUpstreamHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "internal-123")
		w.Header().Set("Server", "internal-server")
		w.Header().Set("X-Oneapi-Request-Id", "oneapi-456")
		w.Header().Set("X-Custom-Response", "visible")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed, APIKeys: []string{"k"}, Name: "test"},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("X-Request-Id"), "sensitive header should be stripped")
	assert.Empty(t, rec.Header().Get("Server"), "sensitive header should be stripped")
	assert.Empty(t, rec.Header().Get("X-Oneapi-Request-Id"), "sensitive header should be stripped")
	assert.Equal(t, "visible", rec.Header().Get("X-Custom-Response"))
}

// ---------------------------------------------------------------------------
// forwardResponse tests (exercised through ServeHTTP)
// ---------------------------------------------------------------------------

func TestForwardResponse_StreamingSSE(t *testing.T) {
	sseData := "data: {\"id\":\"1\"}\n\ndata: {\"id\":\"2\"}\n\ndata: [DONE]\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if ok {
			for _, chunk := range strings.Split(sseData, "\n\n") {
				if chunk == "" {
					continue
				}
				fmt.Fprintf(w, "%s\n\n", chunk)
				flusher.Flush()
			}
		}
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed, APIKeys: []string{"k"}, Name: "streaming"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4","stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "no", rec.Header().Get("X-Accel-Buffering"))
	assert.Contains(t, rec.Body.String(), "data:")
}

func TestForwardResponse_ErrorResponse_Sanitized(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid request with key [sk-abc123def456]"}`))
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed, APIKeys: []string{"k"}, Name: "test"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	body := rec.Body.String()
	assert.NotContains(t, body, "sk-abc123def456", "API key should be sanitized")
	assert.Contains(t, body, "[***]", "sanitized placeholder should be present")
}

func TestForwardResponse_Non2xx_SetsContentLength(t *testing.T) {
	errBody := `{"error":"something went wrong"}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(errBody))
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed, APIKeys: []string{"k"}, Name: "test"},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	// Content-Length should be set after sanitization.
	cl := rec.Header().Get("Content-Length")
	assert.NotEmpty(t, cl, "Content-Length should be set for error responses")
}

func TestForwardResponse_InternalHeaders_Set(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed, APIKeys: []string{"k1", "k2"}, KeyRowIDs: []int64{10, 20}, Name: "my-upstream", KeySchedulingMode: "round-robin"},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "my-upstream", rec.Header().Get("X-Upstream-Name"))
	assert.Equal(t, "0", rec.Header().Get("X-API-Key-Index"))
}

// ---------------------------------------------------------------------------
// classifyUpstreamFailure additional coverage
// ---------------------------------------------------------------------------

func TestClassifyUpstreamFailure_AllStatuses(t *testing.T) {
	tests := []struct {
		status int
		want   FailureKind
	}{
		{401, FailureAuth},
		{403, FailureAuth},
		{502, FailureServerError},
		{503, FailureServerError},
		{504, FailureServerError},
		{200, FailureNone},
		{400, FailureNone},
		{500, FailureNone},
		{404, FailureNone},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("status_%d", tc.status), func(t *testing.T) {
			got := classifyUpstreamFailure(tc.status, nil, nil)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestClassifyUpstreamFailure_429_TransientRateLimit(t *testing.T) {
	// With Retry-After + body that doesn't mention quota -> rate limit.
	h := http.Header{}
	h.Set("Retry-After", "60")
	kind := classifyUpstreamFailure(429, h, []byte(`{"error":{"message":"Too many requests"}}`))
	assert.Equal(t, FailureRateLimit, kind)
}

func TestClassifyUpstreamFailure_429_QuotaByStructuredCode(t *testing.T) {
	body := []byte(`{"error":{"code":"insufficient_quota","message":"You have exhausted your quota"}}`)
	kind := classifyUpstreamFailure(429, http.Header{}, body)
	assert.Equal(t, FailureQuota, kind)
}

func TestClassifyUpstreamFailure_429_QuotaByType(t *testing.T) {
	body := []byte(`{"error":{"type":"insufficient_billing","message":"Please check your plan"}}`)
	kind := classifyUpstreamFailure(429, http.Header{}, body)
	assert.Equal(t, FailureQuota, kind)
}

func TestClassifyUpstreamFailure_NetworkError(t *testing.T) {
	// classifyUpstreamFailure only inspects status/headers/body.
	// Network errors are handled by ServeHTTP before classification.
	// This tests the edge case of status 0 (not a standard HTTP status).
	kind := classifyUpstreamFailure(0, nil, nil)
	assert.Equal(t, FailureNone, kind)
}

// ---------------------------------------------------------------------------
// isQuotaExhausted additional coverage
// ---------------------------------------------------------------------------

func TestIsQuotaExhausted_VariousPatterns(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"insufficient_quota keyword", `{"error":"insufficient_quota"}`, true},
		{"exceeded your current quota", `your account has exceeded your current quota`, true},
		{"quota exceeded", `Error: quota exceeded for this key`, true},
		{"billing keyword", `billing issue detected`, true},
		{"credit balance", `Your credit balance is too low`, true},
		{"payment required", `payment required to continue`, true},
		{"spend limit", `spend limit reached`, true},
		{"usage limit", `usage limit exceeded`, true},
		{"normal rate limit message", `Too many requests, please try again later`, false},
		{"empty body no headers no retry", ``, true}, // no headers, no body -> lean quota
		{"unrelated error", `{"error":"invalid model"}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var hdr http.Header
			if tc.body == "" {
				hdr = nil // nil header
			} else {
				hdr = http.Header{}
			}
			got := isQuotaExhausted(hdr, []byte(tc.body))
			assert.Equal(t, tc.want, got, "body=%q", tc.body)
		})
	}
}

func TestIsQuotaExhausted_WithRetryAfterAndBody(t *testing.T) {
	// Retry-After present but body mentions quota -> still quota.
	h := http.Header{}
	h.Set("Retry-After", "30")
	body := []byte(`{"error":{"code":"insufficient_quota","message":"quota exhausted"}}`)
	assert.True(t, isQuotaExhausted(h, body))
}

func TestIsQuotaExhausted_WithRateLimitHeaders_NoQuotaBody(t *testing.T) {
	h := http.Header{}
	h.Set("x-ratelimit-remaining-requests", "0")
	body := []byte(`{"error":{"message":"rate limited"}}`)
	assert.False(t, isQuotaExhausted(h, body), "rate limit headers + non-quota body = not quota")
}

func TestIsQuotaExhausted_EmptyBodyWithRetryAfter(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "5")
	// Empty body + has Retry-After -> not quota.
	assert.False(t, isQuotaExhausted(h, nil))
}

func TestIsQuotaExhausted_EmptyBodyWithRemainingHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("X-RateLimit-Remaining", "10")
	// Empty body + has rate limit remaining header -> not quota.
	assert.False(t, isQuotaExhausted(h, nil))
}

func TestIsQuotaExhausted_StructuredQuotaCode(t *testing.T) {
	body := []byte(`{"error":{"code":"some_quota_error","type":"normal","message":"something"}}`)
	assert.True(t, isQuotaExhausted(http.Header{}, body))
}

// ---------------------------------------------------------------------------
// peekResponseBody additional coverage
// ---------------------------------------------------------------------------

func TestPeekResponseBody_LargeBody_Truncation(t *testing.T) {
	longData := strings.Repeat("x", 1000)
	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader(longData)),
	}
	got := peekResponseBody(resp, 100)
	assert.Len(t, got, 100, "peek should truncate to limit")

	// Remaining data should still be readable.
	remaining, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Len(t, remaining, 1000, "full content should be available after peek (peeked + remainder)")
}

func TestPeekResponseBody_EmptyBody(t *testing.T) {
	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader("")),
	}
	got := peekResponseBody(resp, 100)
	assert.Empty(t, got)
}

func TestPeekResponseBody_NilResponse(t *testing.T) {
	assert.Nil(t, peekResponseBody(nil, 100))
}

func TestPeekResponseBody_NilBody(t *testing.T) {
	resp := &http.Response{Body: nil}
	assert.Nil(t, peekResponseBody(resp, 100))
}

func TestPeekResponseBody_ZeroLimit(t *testing.T) {
	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader("data")),
	}
	got := peekResponseBody(resp, 0)
	assert.Nil(t, got, "zero limit should return nil")
}

func TestPeekResponseBody_NegativeLimit(t *testing.T) {
	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader("data")),
	}
	got := peekResponseBody(resp, -1)
	assert.Nil(t, got, "negative limit should return nil")
}

func TestPeekResponseBody_ExactSize(t *testing.T) {
	data := "hello"
	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader(data)),
	}
	got := peekResponseBody(resp, int64(len(data)))
	assert.Equal(t, data, string(got))

	// Full content should be re-readable.
	all, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, data, string(all))
}

// ---------------------------------------------------------------------------
// stripRequestHopByHop additional coverage
// ---------------------------------------------------------------------------

func TestStripRequestHopByHop_NilHeader(t *testing.T) {
	// Should not panic.
	stripRequestHopByHop(nil)
}

func TestStripRequestHopByHop_NoConnectionHeader(t *testing.T) {
	h := http.Header{}
	h.Set("Proxy-Authenticate", "Basic")
	h.Set("Upgrade", "websocket")
	h.Set("Content-Type", "application/json")

	stripRequestHopByHop(h)

	assert.Empty(t, h.Get("Proxy-Authenticate"))
	assert.Empty(t, h.Get("Upgrade"))
	assert.Equal(t, "application/json", h.Get("Content-Type"))
}

func TestStripRequestHopByHop_AllStandardHopByHop(t *testing.T) {
	h := http.Header{}
	for name := range hopByHopHeaders {
		h.Set(name, "value")
	}
	h.Set("X-Custom", "keep")

	stripRequestHopByHop(h)

	for name := range hopByHopHeaders {
		assert.Empty(t, h.Get(name), "hop-by-hop header %s should be removed", name)
	}
	assert.Equal(t, "keep", h.Get("X-Custom"))
}

func TestStripRequestHopByHop_ConnectionWithMultipleTokens(t *testing.T) {
	h := http.Header{}
	h.Set("Connection", "X-A, X-B, X-C")
	h.Set("X-A", "a")
	h.Set("X-B", "b")
	h.Set("X-C", "c")
	h.Set("X-D", "keep")

	stripRequestHopByHop(h)

	assert.Empty(t, h.Get("Connection"))
	assert.Empty(t, h.Get("X-A"))
	assert.Empty(t, h.Get("X-B"))
	assert.Empty(t, h.Get("X-C"))
	assert.Equal(t, "keep", h.Get("X-D"))
}

// ---------------------------------------------------------------------------
// ProbeNow / GetCurrentID / ActiveRequests
// ---------------------------------------------------------------------------

func TestProbeNow_TriggersProbe(t *testing.T) {
	s := newTestStore(t)
	dp := NewDynamicProxy()

	healthy := healthyServer(t)
	_, err := s.CreateUpstream("probe-target", healthy.URL, []string{"pk"}, 0, "", "", "", "")
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
	_, err := s.CreateUpstream("first", healthy.URL, []string{"k1"}, 0, "", "", "", "")
	require.NoError(t, err)

	prober := NewUpstreamProber(s, dp, time.Minute, 5*time.Second)
	prober.ProbeNow()
	require.Len(t, dp.GetAllUpstreams(), 1)

	// Add another upstream and re-probe.
	healthy2 := healthyServer(t)
	_, err = s.CreateUpstream("second", healthy2.URL, []string{"k2"}, 1, "", "", "", "")
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
	_, err := s.CreateUpstream("test", healthy.URL, []string{"k"}, 0, "", "", "", "")
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
	up, err := s.CreateUpstream("disabled", healthy.URL, []string{"k"}, 0, "", "", "", "")
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
