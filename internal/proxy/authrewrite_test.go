package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRewriteAuthHeaders_OpenAI_SetsBearer(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	RewriteAuthHeaders(r, StyleOpenAI, "upstream-openai-key", "")

	assert.Equal(t, "Bearer upstream-openai-key", r.Header.Get("Authorization"))
	assert.Empty(t, r.Header.Get("x-api-key"))
}

func TestRewriteAuthHeaders_OpenAI_RemovesXApiKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set("x-api-key", "some-downstream-key")

	RewriteAuthHeaders(r, StyleOpenAI, "upstream-openai-key", "")

	assert.Equal(t, "Bearer upstream-openai-key", r.Header.Get("Authorization"))
	assert.Empty(t, r.Header.Get("x-api-key"), "x-api-key must be removed for OpenAI style")
}

func TestRewriteAuthHeaders_OpenAI_OverwritesExistingAuthorization(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set("Authorization", "Bearer old-key")

	RewriteAuthHeaders(r, StyleOpenAI, "new-upstream-key", "")

	assert.Equal(t, "Bearer new-upstream-key", r.Header.Get("Authorization"))
}

func TestRewriteAuthHeaders_Anthropic_SetsXApiKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	RewriteAuthHeaders(r, StyleAnthropic, "upstream-ant-key", "")

	assert.Equal(t, "upstream-ant-key", r.Header.Get("x-api-key"))
	assert.Empty(t, r.Header.Get("Authorization"))
}

func TestRewriteAuthHeaders_Anthropic_RemovesAuthorization(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	r.Header.Set("Authorization", "Bearer downstream-key")

	RewriteAuthHeaders(r, StyleAnthropic, "upstream-ant-key", "")

	assert.Equal(t, "upstream-ant-key", r.Header.Get("x-api-key"))
	assert.Empty(t, r.Header.Get("Authorization"), "Authorization must be removed for Anthropic style")
}

func TestRewriteAuthHeaders_Anthropic_OverwritesExistingXApiKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	r.Header.Set("x-api-key", "old-downstream-key")

	RewriteAuthHeaders(r, StyleAnthropic, "new-upstream-key", "")

	assert.Equal(t, "new-upstream-key", r.Header.Get("x-api-key"))
}

func TestRewriteAuthHeaders_OpenAI_EmptyUpstreamKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	RewriteAuthHeaders(r, StyleOpenAI, "", "")

	assert.Equal(t, "", r.Header.Get("Authorization"))
}

func TestRewriteAuthHeaders_Anthropic_EmptyUpstreamKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	RewriteAuthHeaders(r, StyleAnthropic, "", "")

	assert.Equal(t, "", r.Header.Get("x-api-key"))
}

func TestRewriteAuthHeaders_Anthropic_OAuth_SetsBearer(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	r.Header.Set("x-api-key", "downstream-key")
	RewriteAuthHeaders(r, StyleAnthropic, "sk-ant-oai01-token", AuthModeOAuth)

	assert.Equal(t, "Bearer sk-ant-oai01-token", r.Header.Get("Authorization"))
	assert.Empty(t, r.Header.Get("x-api-key"), "x-api-key must be removed for Anthropic OAuth mode")
	assert.Contains(t, r.Header.Get("anthropic-beta"), AnthropicOAuthBetaOAuth)
	assert.Contains(t, r.Header.Get("anthropic-beta"), AnthropicOAuthBetaClaudeCode)
	assert.Equal(t, AnthropicOAuthUserAgent, r.Header.Get("User-Agent"))
	assert.Equal(t, AnthropicOAuthXApp, r.Header.Get("x-app"))
	assert.Equal(t, "true", r.Header.Get("anthropic-dangerous-direct-browser-access"))
	assert.NotEmpty(t, r.Header.Get("x-claude-code-session-id"))
	assert.Equal(t, AnthropicStainlessLang, r.Header.Get("x-stainless-lang"))
}

func TestEnsureAnthropicOAuthClientHeaders_MergesExistingBetas(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	r.Header.Set("anthropic-beta", "interleaved-thinking-2025-05-14")
	r.Header.Set("User-Agent", "my-client/1.0")
	EnsureAnthropicOAuthClientHeaders(r.Header)

	beta := r.Header.Get("anthropic-beta")
	assert.Contains(t, beta, "interleaved-thinking-2025-05-14")
	assert.Contains(t, beta, AnthropicOAuthBetaOAuth)
	assert.Contains(t, beta, AnthropicOAuthBetaClaudeCode)
	assert.Equal(t, "my-client/1.0", r.Header.Get("User-Agent"), "custom UA must be preserved")
}

func TestEnsureAnthropicOAuthClientHeaders_ReplacesGoUserAgent(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	r.Header.Set("User-Agent", "Go-http-client/1.1")
	EnsureAnthropicOAuthClientHeaders(r.Header)
	assert.Equal(t, AnthropicOAuthUserAgent, r.Header.Get("User-Agent"))
}

func TestRewriteAuthHeaders_Anthropic_APIKeyMode_Default(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	RewriteAuthHeaders(r, StyleAnthropic, "sk-ant-api03-key", AuthModeAPIKey)

	assert.Equal(t, "sk-ant-api03-key", r.Header.Get("x-api-key"))
	assert.Empty(t, r.Header.Get("Authorization"))
}
