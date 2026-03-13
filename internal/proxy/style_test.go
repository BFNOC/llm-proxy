package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newRequest(method, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	return r
}

func TestDetectProviderStyle_MessagesPath(t *testing.T) {
	r := newRequest(http.MethodPost, "/v1/messages")
	assert.Equal(t, StyleAnthropic, DetectProviderStyle(r))
}

func TestDetectProviderStyle_MessagesPathWithSuffix(t *testing.T) {
	r := newRequest(http.MethodPost, "/v1/messages/count_tokens")
	assert.Equal(t, StyleAnthropic, DetectProviderStyle(r))
}

func TestDetectProviderStyle_XApiKeyHeader(t *testing.T) {
	r := newRequest(http.MethodPost, "/v1/chat/completions")
	r.Header.Set("x-api-key", "sk-ant-test")
	assert.Equal(t, StyleAnthropic, DetectProviderStyle(r))
}

func TestDetectProviderStyle_AnthropicVersionHeader(t *testing.T) {
	r := newRequest(http.MethodPost, "/v1/chat/completions")
	r.Header.Set("anthropic-version", "2023-06-01")
	assert.Equal(t, StyleAnthropic, DetectProviderStyle(r))
}

func TestDetectProviderStyle_BothAnthropicHeaders(t *testing.T) {
	r := newRequest(http.MethodPost, "/v1/chat/completions")
	r.Header.Set("x-api-key", "sk-ant-test")
	r.Header.Set("anthropic-version", "2023-06-01")
	assert.Equal(t, StyleAnthropic, DetectProviderStyle(r))
}

func TestDetectProviderStyle_DefaultOpenAI(t *testing.T) {
	r := newRequest(http.MethodPost, "/v1/chat/completions")
	r.Header.Set("Authorization", "Bearer sk-openai-test")
	assert.Equal(t, StyleOpenAI, DetectProviderStyle(r))
}

func TestDetectProviderStyle_NoHeadersNoPath(t *testing.T) {
	r := newRequest(http.MethodPost, "/v1/completions")
	assert.Equal(t, StyleOpenAI, DetectProviderStyle(r))
}

func TestDetectProviderStyle_PathPrecedenceOverDefault(t *testing.T) {
	// Path /v1/messages takes precedence even if Authorization is also set
	r := newRequest(http.MethodPost, "/v1/messages")
	r.Header.Set("Authorization", "Bearer sk-openai-test")
	assert.Equal(t, StyleAnthropic, DetectProviderStyle(r))
}

func TestDetectProviderStyle_GetRequest(t *testing.T) {
	r := newRequest(http.MethodGet, "/v1/models")
	assert.Equal(t, StyleOpenAI, DetectProviderStyle(r))
}

func TestDetectProviderStyle_EmptyXApiKey(t *testing.T) {
	// An explicitly empty x-api-key header should NOT trigger Anthropic detection
	r := newRequest(http.MethodPost, "/v1/chat/completions")
	r.Header.Set("x-api-key", "")
	assert.Equal(t, StyleOpenAI, DetectProviderStyle(r))
}
