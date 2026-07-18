package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFullRecordingPolicy_KeySelection(t *testing.T) {
	policy := NewFullRecordingPolicy(store.FullRecordingConfig{})
	assert.False(t, policy.ShouldRecord(1))

	policy.Update(store.FullRecordingConfig{Enabled: true, AllKeys: true})
	assert.True(t, policy.ShouldRecord(1))
	assert.True(t, policy.ShouldRecord(999))

	policy.Update(store.FullRecordingConfig{Enabled: true, AllKeys: false, DownstreamKeyIDs: []int64{2}})
	assert.False(t, policy.ShouldRecord(1))
	assert.True(t, policy.ShouldRecord(2))

	policy.Update(store.FullRecordingConfig{Enabled: true, AllKeys: false})
	assert.False(t, policy.ShouldRecord(1), "指定范围为空时必须匹配零个 Key")
}

func TestExtractSessionMetadata_ProtocolSources(t *testing.T) {
	t.Run("Codex session header outranks thread", func(t *testing.T) {
		header := http.Header{"Session-Id": {"session-root"}, "Thread-Id": {"thread-child"}}
		metadata := extractSessionMetadata(header, []byte(`{"prompt_cache_key":"cache"}`), nil, 1, "responses", "/v1/responses")
		assert.Equal(t, "session-root", metadata.ID)
		assert.Equal(t, "header:session-id", metadata.Source)
	})

	t.Run("Responses conversation and parent response", func(t *testing.T) {
		request := []byte(`{"conversation":"conv_123","previous_response_id":"resp_parent","input":"next"}`)
		response := []byte("event: response.created\ndata: {\"response\":{\"id\":\"resp_child\"}}\n\n")
		metadata := extractSessionMetadata(nil, request, response, 1, "responses", "/v1/responses")
		assert.Equal(t, "conv_123", metadata.ID)
		assert.Equal(t, "body:conversation", metadata.Source)
		assert.Equal(t, "resp_parent", metadata.ParentResponseID)
		assert.Equal(t, "resp_child", metadata.ResponseID)
	})

	t.Run("Claude Code metadata user id", func(t *testing.T) {
		request := []byte(`{"metadata":{"user_id":"{\"device_id\":\"d\",\"session_id\":\"claude-session\"}"},"messages":[{"role":"user","content":"hello"}]}`)
		metadata := extractSessionMetadata(nil, request, nil, 1, "anthropic", "/v1/messages")
		assert.Equal(t, "claude-session", metadata.ID)
		assert.Equal(t, "body:metadata.user_id.session_id", metadata.Source)
	})

	t.Run("Stateless growing history keeps derived session", func(t *testing.T) {
		first := []byte(`{"system":"help","messages":[{"role":"user","content":"same root"}]}`)
		second := []byte(`{"system":"help","messages":[{"role":"user","content":"same root"},{"role":"assistant","content":"answer"},{"role":"user","content":"next"}]}`)
		firstMetadata := extractSessionMetadata(nil, first, nil, 7, "openai", "/v1/chat/completions")
		secondMetadata := extractSessionMetadata(nil, second, nil, 7, "openai", "/v1/chat/completions")
		assert.Equal(t, firstMetadata.ID, secondMetadata.ID)
		assert.Equal(t, "derived:message_root", firstMetadata.Source)
		assert.Equal(t, "same root", firstMetadata.Preview)
	})
}

func TestAuditLogMiddleware_FullRecordSanitizesAndPreservesBody(t *testing.T) {
	s := newAuditMWTestStore(t)
	_, key, err := s.CreateKey("full-record", 0)
	require.NoError(t, err)
	policy := NewFullRecordingPolicy(store.FullRecordingConfig{Enabled: true, AllKeys: false, DownstreamKeyIDs: []int64{key.ID}})
	logger := NewAuditLogger(s, nil, 10, 1, time.Hour)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		require.NoError(t, readErr)
		assert.JSONEq(t, `{"messages":[{"role":"user","content":"hello"}]}`, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","choices":[]}`))
	})
	middleware := AuditLogMiddleware(logger, policy)(inner)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"hello"}]}`))
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("X-API-Key", "secret")
	request.Header.Set("Cookie", "session=secret")
	request.Header.Set("X-Claude-Code-Session-Id", "claude-session")
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(context.WithValue(request.Context(), ctxKeyDownstreamKeyID, key.ID))
	recorder := httptest.NewRecorder()
	middleware.ServeHTTP(recorder, request)
	logger.Stop()
	assert.Zero(t, atomic.LoadInt64(&logger.fullRecordMem.used))

	logs, err := s.QueryLogs(key.ID, time.Now().Add(-time.Minute), time.Now().Add(time.Minute), 10)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.True(t, logs[0].HasFullRecord)
	detail, err := s.GetRequestLogDetail(logs[0].ID)
	require.NoError(t, err)
	assert.Contains(t, detail.RequestHeadersJSON, "Content-Type")
	assert.Contains(t, detail.RequestHeadersJSON, "Authorization")
	assert.Contains(t, detail.RequestHeadersJSON, "X-Claude-Code-Session-Id")
	assert.Contains(t, detail.RequestHeadersJSON, "[REDACTED]")
	assert.NotContains(t, detail.RequestHeadersJSON, "Bearer secret")
	assert.NotContains(t, detail.RequestHeadersJSON, "session=secret")
	assert.JSONEq(t, `{"messages":[{"role":"user","content":"hello"}]}`, detail.RequestBody)
	assert.JSONEq(t, `{"id":"chatcmpl_1","choices":[]}`, detail.ResponseBody)
	assert.Equal(t, "header:x-claude-code-session-id", detail.SessionSource)
	assert.Equal(t, "claude-session", detail.SessionID)
}

func TestFullRecordSanitizers_RedactQueryAndCredentials(t *testing.T) {
	query := sanitizeRawQuery("model=gpt-5&api_key=top-secret&session_id=session-1&access_token=token-value&auth=jwt-secret")
	assert.Contains(t, query, "model=gpt-5")
	assert.Contains(t, query, "session_id=session-1")
	assert.NotContains(t, query, "top-secret")
	assert.NotContains(t, query, "token-value")
	assert.NotContains(t, query, "jwt-secret")

	headers := sanitizeResponseHeaders(http.Header{
		"Content-Type":       {"application/json"},
		"Set-Cookie":         {"sid=secret"},
		"X-Provider-Api-Key": {"provider-secret"},
		"X-Auth":             {"jwt-secret"},
		"X-Request-Id":       {"request-1"},
	})
	assert.Equal(t, "application/json", headers.Get("Content-Type"))
	assert.Equal(t, "request-1", headers.Get("X-Request-Id"))
	assert.Equal(t, "[REDACTED]", headers.Get("Set-Cookie"))
	assert.Equal(t, "[REDACTED]", headers.Get("X-Provider-Api-Key"))
	assert.Equal(t, "[REDACTED]", headers.Get("X-Auth"))
}

func TestAuditLogMiddleware_PanicReleasesCaptureBudget(t *testing.T) {
	s := newAuditMWTestStore(t)
	_, key, err := s.CreateKey("panic-budget", 0)
	require.NoError(t, err)
	policy := NewFullRecordingPolicy(store.FullRecordingConfig{Enabled: true, AllKeys: false, DownstreamKeyIDs: []int64{key.ID}})
	logger := NewAuditLogger(s, nil, 10, 1, time.Hour)
	defer logger.Stop()

	middleware := AuditLogMiddleware(logger, policy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr := io.ReadAll(r.Body)
		require.NoError(t, readErr)
		_, _ = w.Write([]byte(strings.Repeat("r", 1024)))
		panic("test panic")
	}))
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(strings.Repeat("q", 1024)))
	request = request.WithContext(context.WithValue(request.Context(), ctxKeyDownstreamKeyID, key.ID))

	var recovered interface{}
	func() {
		defer func() { recovered = recover() }()
		middleware.ServeHTTP(httptest.NewRecorder(), request)
	}()
	assert.Equal(t, "test panic", recovered)
	assert.Zero(t, atomic.LoadInt64(&logger.fullRecordMem.used))
}

func TestExtractResponseID_AllowsLargeSingleLineSSE(t *testing.T) {
	payload := `data: {"response":{"id":"resp_large","output_text":"` + strings.Repeat("x", 2<<20) + `"}}`
	assert.Equal(t, "resp_large", extractResponseID([]byte(payload)))
}

func TestExtractSessionMetadata_DerivedFingerprintUsesFullFirstMessage(t *testing.T) {
	prefix := strings.Repeat("a", 240)
	first := []byte(`{"messages":[{"role":"user","content":"` + prefix + `-one"}]}`)
	second := []byte(`{"messages":[{"role":"user","content":"` + prefix + `-two"}]}`)
	firstMetadata := extractSessionMetadata(nil, first, nil, 1, "openai", "/v1/chat/completions")
	secondMetadata := extractSessionMetadata(nil, second, nil, 1, "openai", "/v1/chat/completions")
	assert.NotEqual(t, firstMetadata.ID, secondMetadata.ID)
	assert.Equal(t, firstMetadata.Preview, secondMetadata.Preview)
}

func TestAuditLogMiddleware_UnselectedKeyKeepsLightweightLog(t *testing.T) {
	s := newAuditMWTestStore(t)
	_, selected, err := s.CreateKey("selected", 0)
	require.NoError(t, err)
	_, other, err := s.CreateKey("other", 0)
	require.NoError(t, err)
	policy := NewFullRecordingPolicy(store.FullRecordingConfig{Enabled: true, AllKeys: false, DownstreamKeyIDs: []int64{selected.ID}})
	logger := NewAuditLogger(s, nil, 10, 1, time.Hour)
	middleware := AuditLogMiddleware(logger, policy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"messages":[]}`))
	request = request.WithContext(context.WithValue(request.Context(), ctxKeyDownstreamKeyID, other.ID))
	middleware.ServeHTTP(httptest.NewRecorder(), request)
	logger.Stop()

	logs, err := s.QueryLogs(other.ID, time.Now().Add(-time.Minute), time.Now().Add(time.Minute), 10)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.False(t, logs[0].HasFullRecord)
}
