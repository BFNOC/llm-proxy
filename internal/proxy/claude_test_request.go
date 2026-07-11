package proxy

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Claude Code style constants for admin-panel upstream KEY TESTS only.
// Live Claude Code traffic is reverse-proxied as-is; these helpers never run on that path.
//
// Shape aligns with sub2api account tests (stream + system + metadata.user_id) and
// CLIProxyAPI defaults for platform (MacOS). Session / device IDs are fresh random
// UUIDs per test call — never reuse captured personal session IDs.
const (
	// ClaudeCodeSystemPrompt is required for OAuth mimicry (sub2api / real CLI).
	ClaudeCodeSystemPrompt = "You are Claude Code, Anthropic's official CLI for Claude."

	// DefaultOAuthTestBetaHeader is sub2api's DefaultBetaHeader for OAuth account tests.
	DefaultOAuthTestBetaHeader = "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14"

	// DefaultAPIKeyTestBetaHeader is sub2api's APIKeyBetaHeader (no oauth beta).
	DefaultAPIKeyTestBetaHeader = "claude-code-20250219,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14"

	// DefaultAnthropicTestModel matches sub2api DefaultTestModel (lighter than Opus).
	DefaultAnthropicTestModel = "claude-sonnet-4-5-20250929"
)

// ClaudeCodeTestIdentity holds per-test randomized Claude Code session fields.
// Header X-Claude-Code-Session-Id must match metadata.user_id.session_id.
type ClaudeCodeTestIdentity struct {
	DeviceID  string // 64-char hex
	SessionID string // UUID v4
	UserID    string // JSON metadata.user_id (>= CLI 2.1.78)
}

// NewClaudeCodeTestIdentity generates a fresh device_id + session_id for one admin test.
func NewClaudeCodeTestIdentity() (ClaudeCodeTestIdentity, error) {
	dev := make([]byte, 32)
	if _, err := rand.Read(dev); err != nil {
		return ClaudeCodeTestIdentity{}, err
	}
	deviceID := hex.EncodeToString(dev)
	sessionID := newClaudeCodeSessionID()
	// JSON format used by Claude Code >= 2.1.78 (matches real capture shape).
	b, err := json.Marshal(map[string]string{
		"device_id":    deviceID,
		"account_uuid": "",
		"session_id":   sessionID,
	})
	if err != nil {
		return ClaudeCodeTestIdentity{}, err
	}
	return ClaudeCodeTestIdentity{
		DeviceID:  deviceID,
		SessionID: sessionID,
		UserID:    string(b),
	}, nil
}

// BuildClaudeCodeTestPayload builds a minimal Claude Code–shaped /v1/messages body
// for admin OAuth key tests. Returns the body and the randomized identity so headers
// can share the same session_id.
func BuildClaudeCodeTestPayload(model, prompt string) (body []byte, id ClaudeCodeTestIdentity, err error) {
	if model == "" {
		model = DefaultAnthropicTestModel
	}
	if prompt == "" {
		prompt = "hi"
	}
	id, err = NewClaudeCodeTestIdentity()
	if err != nil {
		return nil, ClaudeCodeTestIdentity{}, err
	}
	payload := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "text",
						"text": prompt,
						"cache_control": map[string]string{
							"type": "ephemeral",
						},
					},
				},
			},
		},
		"system": []map[string]any{
			{
				"type": "text",
				"text": ClaudeCodeSystemPrompt,
				"cache_control": map[string]string{
					"type": "ephemeral",
				},
			},
		},
		"metadata": map[string]string{
			"user_id": id.UserID,
		},
		"max_tokens":  1024,
		"temperature": 1,
		"stream":      true,
	}
	body, err = json.Marshal(payload)
	if err != nil {
		return nil, ClaudeCodeTestIdentity{}, err
	}
	return body, id, nil
}

// ApplyClaudeCodeTestHeaders sets Claude Code client headers for an outbound
// Anthropic admin test request. sessionID must match body metadata.user_id.session_id
// (pass identity from BuildClaudeCodeTestPayload). Empty sessionID generates a new one
// (avoid this for OAuth tests — header/body would desync).
//
// Platform fingerprint uses MacOS (CLIProxyAPI default / real macOS Claude Code).
// Session-like IDs are always random for this call — not from a live capture.
func ApplyClaudeCodeTestHeaders(h http.Header, oauth bool, sessionID string) {
	if h == nil {
		return
	}
	if sessionID == "" {
		sessionID = newClaudeCodeSessionID()
	}
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json")
	h.Set("Accept-Language", "*")
	h.Set("anthropic-version", "2023-06-01")
	h.Set("User-Agent", AnthropicOAuthUserAgent)
	h.Set("X-App", AnthropicOAuthXApp)
	h.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")
	h.Set("X-Claude-Code-Session-Id", sessionID)
	// Per-request id (real Claude Code generates a new one every call).
	h.Set("x-client-request-id", newClaudeCodeSessionID())
	h.Set("X-Stainless-Arch", AnthropicStainlessArch)
	h.Set("X-Stainless-Lang", AnthropicStainlessLang)
	h.Set("X-Stainless-OS", AnthropicStainlessOS) // MacOS
	h.Set("X-Stainless-Package-Version", AnthropicStainlessPackageVersion)
	h.Set("X-Stainless-Runtime", AnthropicStainlessRuntime)
	h.Set("X-Stainless-Runtime-Version", AnthropicStainlessRuntimeVersion)
	h.Set("X-Stainless-Timeout", AnthropicStainlessTimeout)
	h.Set("X-Stainless-Retry-Count", "0")
	if oauth {
		h.Set("anthropic-beta", DefaultOAuthTestBetaHeader)
	} else {
		h.Set("anthropic-beta", DefaultAPIKeyTestBetaHeader)
	}
}

// AnthropicMessagesTestURL builds the test URL. OAuth tests use ?beta=true like sub2api.
func AnthropicMessagesTestURL(baseURL string, oauth bool) string {
	u := strings.TrimRight(baseURL, "/") + "/v1/messages"
	if oauth {
		return u + "?beta=true"
	}
	return u
}

// ParseAnthropicStreamReply extracts concatenated text deltas from an Anthropic SSE body.
// Also works if the body is a single JSON message (non-stream).
func ParseAnthropicStreamReply(body []byte) (reply string, model string) {
	// Non-stream JSON fallback
	var msg struct {
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(body, &msg) == nil && len(msg.Content) > 0 {
		var b strings.Builder
		for _, c := range msg.Content {
			if c.Type == "text" || c.Text != "" {
				b.WriteString(c.Text)
			}
		}
		if b.Len() > 0 {
			return b.String(), msg.Model
		}
	}

	var text strings.Builder
	sc := bufio.NewScanner(bytes.NewReader(body))
	// SSE lines can be long; raise buffer.
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" || data == "" {
			continue
		}
		var ev struct {
			Type  string `json:"type"`
			Model string `json:"model"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			ContentBlock struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content_block"`
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			continue
		}
		if ev.Model != "" {
			model = ev.Model
		}
		if ev.Message.Model != "" {
			model = ev.Message.Model
		}
		switch ev.Type {
		case "content_block_delta":
			if ev.Delta.Text != "" {
				text.WriteString(ev.Delta.Text)
			}
		case "content_block_start":
			if ev.ContentBlock.Text != "" {
				text.WriteString(ev.ContentBlock.Text)
			}
		}
	}
	return text.String(), model
}

// DrainAndParseAnthropicReply reads up to limit bytes and parses stream or JSON reply.
func DrainAndParseAnthropicReply(r io.Reader, limit int64) (reply, model string, raw []byte, err error) {
	raw, err = io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return "", "", raw, err
	}
	reply, model = ParseAnthropicStreamReply(raw)
	return reply, model, raw, nil
}
