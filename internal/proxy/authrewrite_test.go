package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRewriteAuthHeaders_OpenAI_SetsBearer(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	RewriteAuthHeaders(r, StyleOpenAI, "upstream-openai-key")

	assert.Equal(t, "Bearer upstream-openai-key", r.Header.Get("Authorization"))
	assert.Empty(t, r.Header.Get("x-api-key"))
}

func TestRewriteAuthHeaders_OpenAI_RemovesXApiKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set("x-api-key", "some-downstream-key")

	RewriteAuthHeaders(r, StyleOpenAI, "upstream-openai-key")

	assert.Equal(t, "Bearer upstream-openai-key", r.Header.Get("Authorization"))
	assert.Empty(t, r.Header.Get("x-api-key"), "x-api-key must be removed for OpenAI style")
}

func TestRewriteAuthHeaders_OpenAI_OverwritesExistingAuthorization(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set("Authorization", "Bearer old-key")

	RewriteAuthHeaders(r, StyleOpenAI, "new-upstream-key")

	assert.Equal(t, "Bearer new-upstream-key", r.Header.Get("Authorization"))
}

func TestRewriteAuthHeaders_Anthropic_SetsXApiKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	RewriteAuthHeaders(r, StyleAnthropic, "upstream-ant-key")

	assert.Equal(t, "upstream-ant-key", r.Header.Get("x-api-key"))
	assert.Empty(t, r.Header.Get("Authorization"))
}

func TestRewriteAuthHeaders_Anthropic_RemovesAuthorization(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	r.Header.Set("Authorization", "Bearer downstream-key")

	RewriteAuthHeaders(r, StyleAnthropic, "upstream-ant-key")

	assert.Equal(t, "upstream-ant-key", r.Header.Get("x-api-key"))
	assert.Empty(t, r.Header.Get("Authorization"), "Authorization must be removed for Anthropic style")
}

func TestRewriteAuthHeaders_Anthropic_OverwritesExistingXApiKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	r.Header.Set("x-api-key", "old-downstream-key")

	RewriteAuthHeaders(r, StyleAnthropic, "new-upstream-key")

	assert.Equal(t, "new-upstream-key", r.Header.Get("x-api-key"))
}

func TestRewriteAuthHeaders_OpenAI_EmptyUpstreamKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	RewriteAuthHeaders(r, StyleOpenAI, "")

	assert.Equal(t, "Bearer ", r.Header.Get("Authorization"))
}

func TestRewriteAuthHeaders_Anthropic_EmptyUpstreamKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	RewriteAuthHeaders(r, StyleAnthropic, "")

	assert.Equal(t, "", r.Header.Get("x-api-key"))
}
