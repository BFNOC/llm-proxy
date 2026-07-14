package proxy

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Codex 客户端指纹，仅用于 admin 面板的 KEY 测试（protocol=responses + client_spoof）。
// 真实的 Codex → proxy 流量按原样反向代理；这些辅助函数从不在那条路径上运行。
//
// 请求头/请求体结构对齐一次真实 codex-tui → llm-proxy 的抓包（2026-07-11）：
//   UA: codex-tui/0.144.1 (Mac OS 15.7.3; arm64) …
//   Originator: codex-tui
//   Session-Id / Thread-Id / X-Client-Request-Id 共用一个 UUID
//   X-Codex-Window-Id: {session}:0
//   X-Codex-Turn-Metadata + body.client_metadata / prompt_cache_key
// Session / installation / turn ID 每次测试都全新随机生成 —— 绝不复用真实抓包中的值。
const (
	// CodexCLIUserAgent 对应 macOS arm64（ghostty 终端）上真实的 codex-tui。
	CodexCLIUserAgent = "codex-tui/0.144.1 (Mac OS 15.7.3; arm64) ghostty/1.3.2 (codex-tui; 0.144.1)"

	// CodexOriginator 必须与 User-Agent 中的产品名（codex-tui）配对。
	CodexOriginator = "codex-tui"

	// CodexBetaFeatures 来自真实的 X-Codex-Beta-Features 请求头。
	CodexBetaFeatures = "memories,remote_compaction_v2"

	// DefaultCodexTestModel 与观测到的桌面版 Codex 流量一致。
	DefaultCodexTestModel = "gpt-5.5"

	// 用于连通性探测的简短系统提示词（真实 CLI 会携带一个巨大的 instructions blob）。
	codexTestInstructions = "You are Codex, a coding agent based on GPT-5."
)

// CodexTestIdentity 保存单次测试中随机生成的 Codex session 字段。
// 真实客户端会把 Session-Id、Thread-Id、prompt_cache_key 和 X-Client-Request-Id 绑定在一起。
type CodexTestIdentity struct {
	SessionID      string // UUID → Session-Id, Thread-Id, prompt_cache_key, X-Client-Request-Id
	InstallationID string // UUID → client_metadata + turn metadata 中使用
	TurnID         string // UUID → 每次请求的 turn
	WindowID       string // "{session}:0"
}

// NewCodexTestIdentity 为单次 admin 测试生成全新的 id。
func NewCodexTestIdentity() CodexTestIdentity {
	session := newClaudeCodeSessionID()
	return CodexTestIdentity{
		SessionID:      session,
		InstallationID: newClaudeCodeSessionID(),
		TurnID:         newClaudeCodeSessionID(),
		WindowID:       session + ":0",
	}
}

// codexTestToolsSubset 是一个结构类似真实 codex-tui 流量的极简工具列表。
// 完整的 CLI 会携带 14+ 个工具、多 KB 大小的 schema；探测请求只需要几个
// 有代表性的名称/类型，就足以让上游的指纹识别仍然判定为 "Codex"。
func codexTestToolsSubset() []map[string]any {
	return []map[string]any{
		{
			"type":        "function",
			"name":        "exec_command",
			"description": "Runs a command in a PTY, returning output.",
			"strict":      false,
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"cmd":     map[string]any{"type": "string", "description": "Command to run"},
					"workdir": map[string]any{"type": "string", "description": "Working directory"},
				},
				"required":             []string{"cmd"},
				"additionalProperties": false,
			},
		},
		{
			// 自由格式的 patch 工具是一个很强的 Codex 指纹特征（type=custom）。
			"type":        "custom",
			"name":        "apply_patch",
			"description": "Use the apply_patch tool to edit files.",
			"format": map[string]any{
				"type":   "grammar",
				"syntax": "lark",
				"definition": "start: begin_patch hunk+ end_patch\n" +
					"begin_patch: \"*** Begin Patch\" LF\n" +
					"end_patch: \"*** End Patch\" LF?\n" +
					"hunk: add_hunk | delete_hunk | update_hunk\n" +
					"add_hunk: \"*** Add File: \" filename LF add_line+\n" +
					"delete_hunk: \"*** Delete File: \" filename LF\n" +
					"update_hunk: \"*** Update File: \" filename LF change_line+\n" +
					"add_line: \"+\" /[^\\n]*/ LF\n" +
					"change_line: (\" \" | \"+\" | \"-\") /[^\\n]*/ LF\n" +
					"filename: /[^\\n]+/\n" +
					"LF: \"\\n\"\n",
			},
		},
		{
			"type":        "function",
			"name":        "update_plan",
			"description": "Updates the task plan.",
			"strict":      false,
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"explanation": map[string]any{"type": "string"},
					"plan": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"step":   map[string]any{"type": "string"},
								"status": map[string]any{"type": "string"},
							},
							"required":             []string{"step", "status"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"plan"},
				"additionalProperties": false,
			},
		},
		{
			// 出现在真实的 codex-tui 抓包中；用于让 tool_choice 的候选面不为空。
			"type":                 "web_search",
			"external_web_access":  false,
			"search_content_types": []string{"text", "image"},
		},
	}
}

// BuildCodexResponsesTestPayload 构建一个符合 Codex 格式的 /v1/responses 请求体，
// 供 admin 测试使用。包含 identity 字段 + 一小部分工具子集；省略了完整的多 KB
// CLI 数据。stream=false 使 admin 端解析响应更简单。
func BuildCodexResponsesTestPayload(model, prompt string) (body []byte, id CodexTestIdentity, err error) {
	if model == "" {
		model = DefaultCodexTestModel
	}
	if prompt == "" {
		prompt = "hi"
	}
	id = NewCodexTestIdentity()

	turnMeta, err := json.Marshal(map[string]any{
		"installation_id":         id.InstallationID,
		"session_id":              id.SessionID,
		"thread_id":               id.SessionID,
		"turn_id":                 id.TurnID,
		"window_id":               id.WindowID,
		"request_kind":            "turn",
		"thread_source":           "user",
		"sandbox":                 "seatbelt",
		"workspaces":              map[string]any{},
		"turn_started_at_unix_ms": time.Now().UnixMilli(),
	})
	if err != nil {
		return nil, CodexTestIdentity{}, err
	}

	payload := map[string]any{
		"model": model,
		// 真实的 codex-tui 会把消息包装为
		// {type:"message", role, content:[{type:input_text,text}]}。
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{
						"type": "input_text",
						"text": prompt,
					},
				},
			},
		},
		"instructions":        codexTestInstructions,
		"tools":               codexTestToolsSubset(),
		"store":               false,
		"stream":              false,
		"parallel_tool_calls": true,
		"tool_choice":         "auto",
		"prompt_cache_key":    id.SessionID,
		"reasoning":           map[string]string{"effort": "low"},
		"text":                map[string]string{"verbosity": "low"},
		"include":             []string{"reasoning.encrypted_content"},
		"client_metadata": map[string]string{
			"x-codex-installation-id": id.InstallationID,
			"session_id":              id.SessionID,
			"x-codex-window-id":       id.WindowID,
			"thread_id":               id.SessionID,
			"turn_id":                 id.TurnID,
			"x-codex-turn-metadata":   string(turnMeta),
		},
	}
	body, err = json.Marshal(payload)
	if err != nil {
		return nil, CodexTestIdentity{}, err
	}
	return body, id, nil
}

// ApplyCodexTestHeaders 为外发的 responses 测试请求设置类似 codex-tui 的请求头。
// identity 必须来自 BuildCodexResponsesTestPayload，以确保请求头和请求体的 ID 一致。
func ApplyCodexTestHeaders(h http.Header, id CodexTestIdentity) {
	if h == nil {
		return
	}
	if id.SessionID == "" {
		id = NewCodexTestIdentity()
	}
	if id.WindowID == "" {
		id.WindowID = id.SessionID + ":0"
	}
	if id.InstallationID == "" {
		id.InstallationID = newClaudeCodeSessionID()
	}
	if id.TurnID == "" {
		id.TurnID = newClaudeCodeSessionID()
	}

	turnMeta, _ := json.Marshal(map[string]any{
		"installation_id":         id.InstallationID,
		"session_id":              id.SessionID,
		"thread_id":               id.SessionID,
		"turn_id":                 id.TurnID,
		"window_id":               id.WindowID,
		"request_kind":            "turn",
		"thread_source":           "user",
		"sandbox":                 "seatbelt",
		"workspaces":              map[string]any{},
		"turn_started_at_unix_ms": time.Now().UnixMilli(),
	})

	h.Set("Content-Type", "application/json")
	// 真实抓包使用 text/event-stream + stream:true；我们为了便于解析使用非流式。
	h.Set("Accept", "application/json")
	h.Set("User-Agent", CodexCLIUserAgent)
	h.Set("Originator", CodexOriginator)
	// 来自 codex-tui 抓包的真实请求头名称（带连字符）。
	h.Set("Session-Id", id.SessionID)
	h.Set("Thread-Id", id.SessionID)
	// 真实客户端在第一轮对话中会把 session UUID 复用为 x-client-request-id。
	h.Set("X-Client-Request-Id", id.SessionID)
	h.Set("X-Codex-Beta-Features", CodexBetaFeatures)
	h.Set("X-Codex-Window-Id", id.WindowID)
	h.Set("X-Codex-Turn-Metadata", string(turnMeta))
}

// DetectInboundClientFamily 对捕获到的 /v1 请求进行分类，供 admin UI 徽标使用。
// 返回 "claude_code"、"codex" 或 "other"。
func DetectInboundClientFamily(path string, flatHeaders map[string]string) string {
	ua := ""
	originator := ""
	if flatHeaders != nil {
		for k, v := range flatHeaders {
			lk := strings.ToLower(k)
			switch lk {
			case "user-agent":
				ua = v
			case "originator":
				originator = v
			}
		}
	}
	uaLower := strings.ToLower(ua)
	origLower := strings.ToLower(originator)
	pathLower := strings.ToLower(path)

	// Codex 信号（codex-tui / codex_cli_rs / vscode）
	if strings.HasPrefix(uaLower, "codex_cli_rs/") ||
		strings.HasPrefix(uaLower, "codex-tui/") ||
		strings.HasPrefix(uaLower, "codex_vscode/") ||
		strings.HasPrefix(uaLower, "codex_vscode_copilot/") ||
		origLower == "codex_cli_rs" ||
		origLower == "codex-tui" ||
		origLower == "codex_vscode" ||
		strings.Contains(uaLower, "codex") ||
		flatHas(flatHeaders, "x-codex-window-id") ||
		flatHas(flatHeaders, "x-codex-turn-metadata") ||
		flatHas(flatHeaders, "x-codex-beta-features") {
		return "codex"
	}
	// Claude Code 信号
	if strings.HasPrefix(uaLower, "claude-cli/") ||
		strings.Contains(uaLower, "claude-cli/") ||
		flatHas(flatHeaders, "x-claude-code-session-id") ||
		flatHas(flatHeaders, "anthropic-beta") && strings.Contains(strings.ToLower(flatGet(flatHeaders, "anthropic-beta")), "claude-code") {
		return "claude_code"
	}
	// 仅凭路径的弱提示
	if strings.Contains(pathLower, "/responses") && (flatHas(flatHeaders, "openai-beta") || flatHas(flatHeaders, "session-id")) {
		return "codex"
	}
	return "other"
}

func flatHas(m map[string]string, name string) bool {
	return flatGet(m, name) != ""
}

func flatGet(m map[string]string, name string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[name]; ok {
		return v
	}
	want := strings.ToLower(name)
	for k, v := range m {
		if strings.ToLower(k) == want {
			return v
		}
	}
	return ""
}

