package proxy

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildClaudeCodeTestPayload_Shape(t *testing.T) {
	raw, id, err := BuildClaudeCodeTestPayload("claude-opus-4-6", "hi")
	require.NoError(t, err)
	require.NotEmpty(t, id.SessionID)
	require.NotEmpty(t, id.DeviceID)
	require.Len(t, id.DeviceID, 64)

	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	assert.Equal(t, "claude-opus-4-6", m["model"])
	assert.Equal(t, true, m["stream"])
	assert.EqualValues(t, 1024, m["max_tokens"])
	sys, ok := m["system"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, sys)
	first := sys[0].(map[string]any)
	assert.Equal(t, ClaudeCodeSystemPrompt, first["text"])
	meta := m["metadata"].(map[string]any)
	assert.Equal(t, id.UserID, meta["user_id"])

	// metadata.user_id is JSON with matching session_id
	var uid map[string]string
	require.NoError(t, json.Unmarshal([]byte(id.UserID), &uid))
	assert.Equal(t, id.SessionID, uid["session_id"])
	assert.Equal(t, id.DeviceID, uid["device_id"])
}

func TestBuildClaudeCodeTestPayload_RandomizesEachCall(t *testing.T) {
	_, a, err := BuildClaudeCodeTestPayload("m", "p")
	require.NoError(t, err)
	_, b, err := BuildClaudeCodeTestPayload("m", "p")
	require.NoError(t, err)
	assert.NotEqual(t, a.SessionID, b.SessionID)
	assert.NotEqual(t, a.DeviceID, b.DeviceID)
}

func TestApplyClaudeCodeTestHeaders_OAuth(t *testing.T) {
	h := make(http.Header)
	session := "11111111-2222-4333-8444-555555555555"
	ApplyClaudeCodeTestHeaders(h, true, session)
	assert.Equal(t, DefaultOAuthTestBetaHeader, h.Get("anthropic-beta"))
	assert.Equal(t, AnthropicOAuthUserAgent, h.Get("User-Agent"))
	assert.Equal(t, "cli", h.Get("X-App"))
	assert.Equal(t, "true", h.Get("Anthropic-Dangerous-Direct-Browser-Access"))
	assert.Contains(t, h.Get("anthropic-beta"), "oauth-2025-04-20")
	assert.Equal(t, session, h.Get("X-Claude-Code-Session-Id"))
	assert.Equal(t, AnthropicStainlessOS, h.Get("X-Stainless-OS"))
	assert.Equal(t, "MacOS", h.Get("X-Stainless-OS"))
	assert.NotEmpty(t, h.Get("x-client-request-id"))
	// request-id must differ from session
	assert.NotEqual(t, session, h.Get("x-client-request-id"))
}

func TestApplyClaudeCodeTestHeaders_HeaderBodySessionAligned(t *testing.T) {
	body, id, err := BuildClaudeCodeTestPayload("claude-sonnet-4-5-20250929", "hi")
	require.NoError(t, err)
	h := make(http.Header)
	ApplyClaudeCodeTestHeaders(h, true, id.SessionID)
	assert.Equal(t, id.SessionID, h.Get("X-Claude-Code-Session-Id"))

	var m map[string]any
	require.NoError(t, json.Unmarshal(body, &m))
	var uid map[string]string
	require.NoError(t, json.Unmarshal([]byte(m["metadata"].(map[string]any)["user_id"].(string)), &uid))
	assert.Equal(t, h.Get("X-Claude-Code-Session-Id"), uid["session_id"])
}

func TestAnthropicMessagesTestURL(t *testing.T) {
	assert.Equal(t, "https://api.anthropic.com/v1/messages?beta=true",
		AnthropicMessagesTestURL("https://api.anthropic.com/", true))
	assert.Equal(t, "https://api.anthropic.com/v1/messages",
		AnthropicMessagesTestURL("https://api.anthropic.com", false))
}

func TestParseAnthropicStreamReply_SSE(t *testing.T) {
	sse := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-sonnet-4-5-20250929\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hel\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"lo\"}}\n\n"
	reply, model := ParseAnthropicStreamReply([]byte(sse))
	assert.Equal(t, "Hello", reply)
	assert.Equal(t, "claude-sonnet-4-5-20250929", model)
}
