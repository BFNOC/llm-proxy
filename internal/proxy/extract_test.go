package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// ExtractDownstreamKey
// ---------------------------------------------------------------------------

func TestExtractDownstreamKey_OpenAI_BearerToken(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set("Authorization", "Bearer sk-openai-abc123")

	key := ExtractDownstreamKey(r, StyleOpenAI)
	assert.Equal(t, "sk-openai-abc123", key)
}

func TestExtractDownstreamKey_OpenAI_MissingAuthorization(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	key := ExtractDownstreamKey(r, StyleOpenAI)
	assert.Empty(t, key)
}

func TestExtractDownstreamKey_OpenAI_NonBearerAuthorization(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

	key := ExtractDownstreamKey(r, StyleOpenAI)
	assert.Empty(t, key, "non-Bearer authorization should return empty string")
}

func TestExtractDownstreamKey_OpenAI_BearerPrefixOnly(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set("Authorization", "Bearer ")

	key := ExtractDownstreamKey(r, StyleOpenAI)
	assert.Equal(t, "", key)
}

func TestExtractDownstreamKey_Anthropic_XApiKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	r.Header.Set("x-api-key", "sk-ant-abc123")

	key := ExtractDownstreamKey(r, StyleAnthropic)
	assert.Equal(t, "sk-ant-abc123", key)
}

func TestExtractDownstreamKey_Anthropic_MissingXApiKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	key := ExtractDownstreamKey(r, StyleAnthropic)
	assert.Empty(t, key)
}

func TestExtractDownstreamKey_Anthropic_IgnoresAuthorization(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	r.Header.Set("Authorization", "Bearer bearer-only-key")
	r.Header.Set("x-api-key", "ant-key")

	key := ExtractDownstreamKey(r, StyleAnthropic)
	assert.Equal(t, "ant-key", key, "Anthropic style must use x-api-key, not Authorization")
}

func TestExtractDownstreamKey_OpenAI_IgnoresXApiKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set("x-api-key", "should-be-ignored")
	r.Header.Set("Authorization", "Bearer openai-key")

	key := ExtractDownstreamKey(r, StyleOpenAI)
	assert.Equal(t, "openai-key", key, "OpenAI style must use Authorization Bearer, not x-api-key")
}

// ---------------------------------------------------------------------------
// HashKey
// ---------------------------------------------------------------------------

func TestHashKey_KnownValue(t *testing.T) {
	// SHA-256("hello") = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	got := HashKey("hello")
	assert.Equal(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", got)
}

func TestHashKey_EmptyString(t *testing.T) {
	// SHA-256("") = e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
	got := HashKey("")
	assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", got)
}

func TestHashKey_Deterministic(t *testing.T) {
	key := "my-secret-api-key"
	assert.Equal(t, HashKey(key), HashKey(key), "HashKey must be deterministic")
}

func TestHashKey_DifferentInputsDifferentOutputs(t *testing.T) {
	h1 := HashKey("key-a")
	h2 := HashKey("key-b")
	assert.NotEqual(t, h1, h2)
}

func TestHashKey_Length(t *testing.T) {
	// SHA-256 produces 32 bytes -> 64 hex chars
	got := HashKey("any-key")
	assert.Len(t, got, 64)
}
