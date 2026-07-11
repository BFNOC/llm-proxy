package proxy

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Codex client fingerprints for admin-panel KEY TESTS only (protocol=responses + client_spoof).
// Live Codex → proxy traffic is reverse-proxied as-is; these helpers never run on that path.
//
// Header/body shape aligned with a real codex-tui → llm-proxy capture (2026-07-11):
//   UA: codex-tui/0.144.1 (Mac OS 15.7.3; arm64) …
//   Originator: codex-tui
//   Session-Id / Thread-Id / X-Client-Request-Id share one UUID
//   X-Codex-Window-Id: {session}:0
//   X-Codex-Turn-Metadata + body.client_metadata / prompt_cache_key
// Session / installation / turn IDs are freshly random per test — never reuse a live capture.
const (
	// CodexCLIUserAgent matches real codex-tui on macOS arm64 (ghostty terminal).
	CodexCLIUserAgent = "codex-tui/0.144.1 (Mac OS 15.7.3; arm64) ghostty/1.3.2 (codex-tui; 0.144.1)"

	// CodexOriginator must pair with User-Agent product (codex-tui).
	CodexOriginator = "codex-tui"

	// CodexBetaFeatures from real X-Codex-Beta-Features header.
	CodexBetaFeatures = "memories,remote_compaction_v2"

	// DefaultCodexTestModel matches observed desktop Codex traffic.
	DefaultCodexTestModel = "gpt-5.5"

	// Short system prompt for connectivity probes (real CLI ships a huge instructions blob).
	codexTestInstructions = "You are Codex, a coding agent based on GPT-5."
)

// CodexTestIdentity holds per-test randomized Codex session fields.
// Real client ties Session-Id, Thread-Id, prompt_cache_key, and X-Client-Request-Id together.
type CodexTestIdentity struct {
	SessionID      string // UUID → Session-Id, Thread-Id, prompt_cache_key, X-Client-Request-Id
	InstallationID string // UUID → client_metadata + turn metadata
	TurnID         string // UUID → per-request turn
	WindowID       string // "{session}:0"
}

// NewCodexTestIdentity generates fresh ids for one admin test.
func NewCodexTestIdentity() CodexTestIdentity {
	session := newClaudeCodeSessionID()
	return CodexTestIdentity{
		SessionID:      session,
		InstallationID: newClaudeCodeSessionID(),
		TurnID:         newClaudeCodeSessionID(),
		WindowID:       session + ":0",
	}
}

// codexTestToolsSubset is a tiny tool list shaped like real codex-tui traffic.
// Full CLI dumps ship 14+ tools with multi-KB schemas; probes only need a few
// distinctive names/types so upstream fingerprinting still sees "Codex".
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
			// Freeform patch tool is a strong Codex fingerprint (type=custom).
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
			// Present in real codex-tui dumps; keeps the tool_choice surface non-empty.
			"type":                 "web_search",
			"external_web_access":  false,
			"search_content_types": []string{"text", "image"},
		},
	}
}

// BuildCodexResponsesTestPayload builds a Codex-shaped /v1/responses body for admin tests.
// Includes identity fields + a small tools subset; omits the full multi-KB CLI dump.
// stream=false keeps admin reply parsing simple.
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
		// Real codex-tui wraps messages as {type:"message", role, content:[{type:input_text,text}]}.
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

// ApplyCodexTestHeaders sets codex-tui-like headers for an outbound responses test.
// Identity must come from BuildCodexResponsesTestPayload so header/body IDs match.
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
	// Real capture uses text/event-stream with stream:true; we use non-stream for easy parsing.
	h.Set("Accept", "application/json")
	h.Set("User-Agent", CodexCLIUserAgent)
	h.Set("Originator", CodexOriginator)
	// Real wire names (hyphenated) from codex-tui capture.
	h.Set("Session-Id", id.SessionID)
	h.Set("Thread-Id", id.SessionID)
	// Real client reuses session UUID for x-client-request-id on the first turn.
	h.Set("X-Client-Request-Id", id.SessionID)
	h.Set("X-Codex-Beta-Features", CodexBetaFeatures)
	h.Set("X-Codex-Window-Id", id.WindowID)
	h.Set("X-Codex-Turn-Metadata", string(turnMeta))
}

// DetectInboundClientFamily classifies a captured /v1 request for admin UI badges.
// Returns "claude_code", "codex", or "other".
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

	// Codex signals (codex-tui / codex_cli_rs / vscode)
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
	// Claude Code signals
	if strings.HasPrefix(uaLower, "claude-cli/") ||
		strings.Contains(uaLower, "claude-cli/") ||
		flatHas(flatHeaders, "x-claude-code-session-id") ||
		flatHas(flatHeaders, "anthropic-beta") && strings.Contains(strings.ToLower(flatGet(flatHeaders, "anthropic-beta")), "claude-code") {
		return "claude_code"
	}
	// Path-only soft hint
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

