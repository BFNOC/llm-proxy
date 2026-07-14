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

// Claude Code 风格常量，仅用于 admin 面板的上游 KEY 测试。
// 真实的 Claude Code 流量按原样反向代理；这些辅助函数从不在那条路径上运行。
//
// 结构对齐 sub2api 的账号测试（stream + system + metadata.user_id）以及
// CLIProxyAPI 的平台默认值（MacOS）。每次测试调用的 Session / device ID
// 都是全新的随机 UUID —— 绝不复用抓包得到的真实个人 session ID。
const (
	// ClaudeCodeSystemPrompt 是模拟 OAuth（sub2api / 真实 CLI）所必需的。
	ClaudeCodeSystemPrompt = "You are Claude Code, Anthropic's official CLI for Claude."

	// DefaultOAuthTestBetaHeader 是 sub2api 用于 OAuth 账号测试的 DefaultBetaHeader。
	DefaultOAuthTestBetaHeader = "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14"

	// DefaultAPIKeyTestBetaHeader 是 sub2api 的 APIKeyBetaHeader（不含 oauth beta）。
	DefaultAPIKeyTestBetaHeader = "claude-code-20250219,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14"

	// DefaultAnthropicTestModel 与 sub2api 的 DefaultTestModel 一致（比 Opus 更轻量）。
	DefaultAnthropicTestModel = "claude-sonnet-4-5-20250929"
)

// ClaudeCodeTestIdentity 保存单次测试中随机生成的 Claude Code session 字段。
// X-Claude-Code-Session-Id 请求头必须与 metadata.user_id.session_id 一致。
type ClaudeCodeTestIdentity struct {
	DeviceID  string // 64 位十六进制字符串
	SessionID string // UUID v4
	UserID    string // JSON 格式的 metadata.user_id（要求 CLI >= 2.1.78）
}

// NewClaudeCodeTestIdentity 为单次 admin 测试生成全新的 device_id + session_id。
func NewClaudeCodeTestIdentity() (ClaudeCodeTestIdentity, error) {
	dev := make([]byte, 32)
	if _, err := rand.Read(dev); err != nil {
		return ClaudeCodeTestIdentity{}, err
	}
	deviceID := hex.EncodeToString(dev)
	sessionID := newClaudeCodeSessionID()
	// JSON 格式，供 Claude Code >= 2.1.78 使用（与真实抓包结构一致）。
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

// BuildClaudeCodeTestPayload 构建一个精简的、符合 Claude Code 格式的
// /v1/messages 请求体，用于 admin OAuth Key 测试。返回请求体和随机生成的
// identity，以便请求头可以共用同一个 session_id。
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

// ApplyClaudeCodeTestHeaders 为外发的 Anthropic admin 测试请求设置
// Claude Code 客户端请求头。sessionID 必须与请求体的
// metadata.user_id.session_id 一致（传入 BuildClaudeCodeTestPayload 返回的
// identity）。sessionID 为空时会生成一个新的（OAuth 测试应避免这样做——
// 否则请求头和请求体的 ID 会不一致）。
//
// 平台指纹使用 MacOS（CLIProxyAPI 默认值 / 真实 macOS Claude Code）。
// 本次调用中的 session 类 ID 始终是随机生成的 —— 而非来自真实抓包。
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
	// 每次请求独立的 id（真实 Claude Code 每次调用都会生成新的）。
	h.Set("x-client-request-id", newClaudeCodeSessionID())
	h.Set("X-Stainless-Arch", AnthropicStainlessArch)
	h.Set("X-Stainless-Lang", AnthropicStainlessLang)
	h.Set("X-Stainless-OS", AnthropicStainlessOS) // MacOS 系统
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

// AnthropicMessagesTestURL 构建测试用的 URL。与 sub2api 一样，
// OAuth 测试会带上 ?beta=true。
func AnthropicMessagesTestURL(baseURL string, oauth bool) string {
	u := strings.TrimRight(baseURL, "/") + "/v1/messages"
	if oauth {
		return u + "?beta=true"
	}
	return u
}

// ParseAnthropicStreamReply 从 Anthropic 的 SSE 响应体中提取拼接后的文本增量。
// 如果响应体是单个 JSON 消息（非流式），同样能正确处理。
func ParseAnthropicStreamReply(body []byte) (reply string, model string) {
	// 非流式 JSON 的兜底处理
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
	// SSE 行可能很长；调大缓冲区。
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

// DrainAndParseAnthropicReply 最多读取 limit 字节，并解析流式或 JSON 格式的响应。
func DrainAndParseAnthropicReply(r io.Reader, limit int64) (reply, model string, raw []byte, err error) {
	raw, err = io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return "", "", raw, err
	}
	reply, model = ParseAnthropicStreamReply(raw)
	return reply, model, raw, nil
}
