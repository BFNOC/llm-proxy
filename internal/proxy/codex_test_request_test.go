package proxy

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCodexResponsesTestPayload_Shape(t *testing.T) {
	raw, id, err := BuildCodexResponsesTestPayload("gpt-5.5", "hello")
	require.NoError(t, err)
	require.NotEmpty(t, id.SessionID)
	require.NotEmpty(t, id.InstallationID)
	require.NotEmpty(t, id.TurnID)
	assert.Equal(t, id.SessionID+":0", id.WindowID)

	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	assert.Equal(t, "gpt-5.5", m["model"])
	assert.Equal(t, false, m["stream"])
	assert.Equal(t, false, m["store"])
	assert.Equal(t, id.SessionID, m["prompt_cache_key"])

	input, ok := m["input"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, input)
	first := input[0].(map[string]any)
	assert.Equal(t, "message", first["type"])
	assert.Equal(t, "user", first["role"])

	cm := m["client_metadata"].(map[string]any)
	assert.Equal(t, id.SessionID, cm["session_id"])
	assert.Equal(t, id.InstallationID, cm["x-codex-installation-id"])
	assert.Equal(t, id.WindowID, cm["x-codex-window-id"])
	assert.Equal(t, id.TurnID, cm["turn_id"])

	tools, ok := m["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 4)
	names := map[string]string{}
	for _, raw := range tools {
		tmap := raw.(map[string]any)
		typ, _ := tmap["type"].(string)
		name, _ := tmap["name"].(string)
		if name == "" {
			name = typ
		}
		names[name] = typ
	}
	assert.Equal(t, "function", names["exec_command"])
	assert.Equal(t, "custom", names["apply_patch"])
	assert.Equal(t, "function", names["update_plan"])
	assert.Equal(t, "web_search", names["web_search"])
}

func TestBuildCodexResponsesTestPayload_RandomizesSession(t *testing.T) {
	_, a, err := BuildCodexResponsesTestPayload("m", "p")
	require.NoError(t, err)
	_, b, err := BuildCodexResponsesTestPayload("m", "p")
	require.NoError(t, err)
	assert.NotEqual(t, a.SessionID, b.SessionID)
	assert.NotEqual(t, a.InstallationID, b.InstallationID)
	assert.NotEqual(t, a.TurnID, b.TurnID)
}

func TestApplyCodexTestHeaders_MatchesCaptureShape(t *testing.T) {
	id := CodexTestIdentity{
		SessionID:      "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
		InstallationID: "11111111-2222-4333-8444-555555555555",
		TurnID:         "99999999-aaaa-4bbb-8ccc-dddddddddddd",
		WindowID:       "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee:0",
	}
	h := make(http.Header)
	ApplyCodexTestHeaders(h, id)

	assert.Equal(t, CodexCLIUserAgent, h.Get("User-Agent"))
	assert.Equal(t, CodexOriginator, h.Get("Originator"))
	assert.Equal(t, CodexBetaFeatures, h.Get("X-Codex-Beta-Features"))
	assert.Equal(t, id.SessionID, h.Get("Session-Id"))
	assert.Equal(t, id.SessionID, h.Get("Thread-Id"))
	assert.Equal(t, id.SessionID, h.Get("X-Client-Request-Id"))
	assert.Equal(t, id.WindowID, h.Get("X-Codex-Window-Id"))
	assert.Contains(t, h.Get("X-Codex-Turn-Metadata"), id.SessionID)
	assert.Contains(t, h.Get("X-Codex-Turn-Metadata"), id.InstallationID)
	assert.Contains(t, h.Get("X-Codex-Turn-Metadata"), id.TurnID)
	// Real desktop capture does not send OpenAI-Beta on custom base URL.
	assert.Empty(t, h.Get("OpenAI-Beta"))
}

func TestDetectInboundClientFamily(t *testing.T) {
	assert.Equal(t, "claude_code", DetectInboundClientFamily("/v1/messages", map[string]string{
		"User-Agent": "claude-cli/2.1.201 (external, cli)",
		"X-App":      "cli",
	}))
	assert.Equal(t, "codex", DetectInboundClientFamily("/v1/responses", map[string]string{
		"User-Agent": "codex-tui/0.144.1 (Mac OS 15.7.3; arm64) ghostty/1.3.2 (codex-tui; 0.144.1)",
		"Originator": "codex-tui",
	}))
	assert.Equal(t, "codex", DetectInboundClientFamily("/v1/responses", map[string]string{
		"X-Codex-Window-Id": "abc:0",
	}))
	assert.Equal(t, "other", DetectInboundClientFamily("/v1/chat/completions", map[string]string{
		"User-Agent": "curl/8.0",
	}))
}
