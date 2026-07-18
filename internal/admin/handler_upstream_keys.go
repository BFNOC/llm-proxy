package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Instawork/llm-proxy/internal/proxy"
)

// --- 按上游管理各自的 API Key ---

// listUpstreamAPIKeys 返回指定上游的所有 API Key 及启用状态。
func (h *AdminHandler) listUpstreamAPIKeys(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	keys, err := h.store.GetUpstreamAllAPIKeys(id)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	type keyInfo struct {
		RowID   int64  `json:"row_id"`
		Key     string `json:"key"`
		Enabled bool   `json:"enabled"`
	}
	result := make([]keyInfo, len(keys))
	for i, k := range keys {
		result[i] = keyInfo{RowID: k.RowID, Key: k.Key, Enabled: k.Enabled}
	}
	jsonOK(w, result)
}

// setAPIKeyEnabled 启用或禁用指定上游的某个 API Key。
func (h *AdminHandler) setAPIKeyEnabled(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	keyID, err := parseAPIKeyRowID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.store.SetAPIKeyEnabled(id, keyID, req.Enabled); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	// 立即触发一次探活，让 Key 变更马上生效。
	go func() {
		defer func() { recover() }()
		h.prober.ProbeNow()
	}()
	slog.Info("admin: updated api key enabled", "upstream_id", id, "key_id", keyID, "enabled", req.Enabled)
	jsonOK(w, map[string]interface{}{"upstream_id": id, "key_id": keyID, "enabled": req.Enabled})
}

// addUpstreamAPIKeys 为上游追加一个或多个 API Key，不影响现有 Key。
func (h *AdminHandler) addUpstreamAPIKeys(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		APIKey  string   `json:"api_key"`
		APIKeys []string `json:"api_keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	keys := cleanAPIKeys(req.APIKeys)
	if req.APIKey != "" {
		keys = append(keys, normalizeAPIKeyValues(req.APIKey)...)
	}
	keys = cleanAPIKeys(keys)
	if len(keys) == 0 {
		jsonError(w, http.StatusBadRequest, "api_keys is required")
		return
	}
	added, err := h.store.AddUpstreamAPIKeys(id, keys)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	go func() {
		defer func() { recover() }()
		h.prober.ProbeNow()
	}()
	slog.Info("admin: added upstream api keys", "upstream_id", id, "count", len(keys))
	jsonOK(w, map[string]interface{}{"status": "created", "count": len(keys), "api_keys": added})
}

// deleteUpstreamAPIKey 删除上游中的单个 API Key。
func (h *AdminHandler) deleteUpstreamAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	keyID, err := parseAPIKeyRowID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.DeleteUpstreamAPIKey(id, keyID); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	go func() {
		defer func() { recover() }()
		h.prober.ProbeNow()
	}()
	slog.Info("admin: deleted upstream api key", "upstream_id", id, "key_id", keyID)
	jsonOK(w, map[string]interface{}{"status": "deleted", "upstream_id": id, "key_id": keyID})
}

// testUpstreamAPIKey 测试指定上游的某个 API Key，支持选择协议、模型和提示词。
func (h *AdminHandler) testUpstreamAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	keyID, err := parseAPIKeyRowID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req struct {
		Protocol    string `json:"protocol"`      // "openai" | "anthropic" | "responses"
		Model       string `json:"model"`         // 测试模型
		Prompt      string `json:"prompt"`        // 测试提示词
		CFClearance string `json:"cf_clearance"`  // CF 绕过
		CFUserAgent string `json:"cf_user_agent"` // CF 绕过
		ClientSpoof *bool  `json:"client_spoof"`  // 客户端伪装：Claude Code / Codex（仅本测试）
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Protocol == "" {
		req.Protocol = "openai"
	}
	if req.Prompt == "" {
		req.Prompt = "你是什么模型？"
	}
	if req.Model == "" {
		switch req.Protocol {
		case "anthropic":
			// 与 sub2api 的 DefaultTestModel 保持一致（探测用，比 Opus 更轻量）。
			req.Model = proxy.DefaultAnthropicTestModel
		case "responses":
			req.Model = proxy.DefaultCodexTestModel
		default:
			req.Model = "gpt-4o-mini"
		}
	}

	// 获取上游信息
	upstream, err := h.store.GetUpstream(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("upstream %d not found", id))
		return
	}

	// 找到指定 row ID 的 Key（keyID=0 表示无鉴权，跳过查找）
	var targetKey string
	if keyID != 0 {
		keyInfos, err := h.store.GetUpstreamAllAPIKeys(id)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "failed to load api keys")
			return
		}
		for _, ki := range keyInfos {
			if ki.RowID == keyID {
				targetKey = ki.Key
				break
			}
		}
		if targetKey == "" {
			jsonError(w, http.StatusNotFound, fmt.Sprintf("api key %d not found", keyID))
			return
		}
	}

	// 构造请求体
	var body []byte
	var testURL string
	var headers map[string]string
	var claudeIdentity proxy.ClaudeCodeTestIdentity // 仅 admin OAuth+伪装场景使用
	var codexIdentity proxy.CodexTestIdentity       // 仅 admin responses+伪装场景使用
	oauthAnthropic := req.Protocol == "anthropic" && upstream.AuthMode == "oauth"
	// client_spoof：为 true 时构造 Claude Code / Codex 形态的探测请求（仅测试面板使用）。
	// OAuth Anthropic 和 responses（Codex）默认开启；普通 OpenAI/API-key 探测默认关闭。
	clientSpoof := false
	if req.ClientSpoof != nil {
		clientSpoof = *req.ClientSpoof
	} else {
		clientSpoof = oauthAnthropic || req.Protocol == "responses"
	}
	spoofClaude := clientSpoof && oauthAnthropic
	spoofCodex := clientSpoof && req.Protocol == "responses"

	switch req.Protocol {
	case "anthropic":
		// OAuth+伪装：sub2api 风格的 Claude Code 请求体（?beta=true，system/metadata/stream）。
		// 否则：最简 messages 探测请求。绝不影响真实的 CC 代理流量。
		testURL = proxy.AnthropicMessagesTestURL(upstream.BaseURL, spoofClaude)
		if spoofClaude {
			var err error
			body, claudeIdentity, err = proxy.BuildClaudeCodeTestPayload(req.Model, req.Prompt)
			if err != nil {
				jsonOK(w, map[string]interface{}{
					"success": false,
					"error":   fmt.Sprintf("构造 Claude Code 测试体失败: %v", err),
				})
				return
			}
		} else {
			body, _ = json.Marshal(map[string]interface{}{
				"model":      req.Model,
				"max_tokens": 100,
				"messages":   []map[string]string{{"role": "user", "content": req.Prompt}},
			})
		}
		headers = map[string]string{}
		if targetKey != "" {
			if upstream.AuthMode == "oauth" {
				headers["Authorization"] = "Bearer " + targetKey
			} else {
				headers["x-api-key"] = targetKey
			}
		}
	case "responses":
		testURL = strings.TrimRight(upstream.BaseURL, "/") + "/v1/responses"
		if spoofCodex {
			var err error
			body, codexIdentity, err = proxy.BuildCodexResponsesTestPayload(req.Model, req.Prompt)
			if err != nil {
				jsonOK(w, map[string]interface{}{
					"success": false,
					"error":   fmt.Sprintf("构造 Codex 测试体失败: %v", err),
				})
				return
			}
		} else {
			body, _ = json.Marshal(map[string]interface{}{
				"model":  req.Model,
				"input":  req.Prompt,
				"stream": false,
			})
		}
		headers = map[string]string{}
		if targetKey != "" {
			headers["Authorization"] = "Bearer " + targetKey
		}
	default: // openai
		testURL = strings.TrimRight(upstream.BaseURL, "/") + "/v1/chat/completions"
		body, _ = json.Marshal(map[string]interface{}{
			"model":      req.Model,
			"max_tokens": 100,
			"messages":   []map[string]string{{"role": "user", "content": req.Prompt}},
		})
		headers = map[string]string{}
		if targetKey != "" {
			headers["Authorization"] = "Bearer " + targetKey
		}
	}

	// 构造带代理的 HTTP client。
	// OAuth Claude 伪装探测使用 utls（Node.js TLS 指纹）；其余用普通 transport。
	var transport *http.Transport
	if spoofClaude {
		transport, err = proxy.BuildTransportUTLS(upstream.ProxyURL)
	} else {
		transport, err = proxy.BuildTransport(upstream.ProxyURL)
	}
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("invalid proxy config: %v", err),
		})
		return
	}
	timeout := 30 * time.Second
	if spoofClaude || spoofCodex {
		timeout = 90 * time.Second
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	httpReq, err := http.NewRequestWithContext(r.Context(), "POST", testURL, bytes.NewReader(body))
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("构造请求失败: %v", err),
		})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	// 仅当开关开启时才应用客户端伪装 Header（仅测试面板使用）。
	if req.Protocol == "anthropic" && targetKey != "" {
		if spoofClaude {
			proxy.ApplyClaudeCodeTestHeaders(httpReq.Header, true, claudeIdentity.SessionID)
			httpReq.Header.Set("Authorization", "Bearer "+targetKey)
		} else {
			httpReq.Header.Set("anthropic-version", "2023-06-01")
			if upstream.AuthMode == "oauth" {
				httpReq.Header.Set("Authorization", "Bearer "+targetKey)
			}
		}
	}
	if spoofCodex {
		proxy.ApplyCodexTestHeaders(httpReq.Header, codexIdentity)
		if targetKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+targetKey)
		}
	}
	applyCFHeaders(httpReq, req.CFClearance, req.CFUserAgent)

	start := time.Now()
	resp, err := client.Do(httpReq)
	latency := time.Since(start)

	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success":    false,
			"error":      err.Error(),
			"latency_ms": latency.Milliseconds(),
		})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success":     false,
			"error":       fmt.Sprintf("读取响应失败: %v", err),
			"status_code": resp.StatusCode,
			"latency_ms":  latency.Milliseconds(),
		})
		return
	}

	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	result := map[string]interface{}{
		"success":      success,
		"status_code":  resp.StatusCode,
		"latency_ms":   latency.Milliseconds(),
		"model":        req.Model,
		"protocol":     req.Protocol,
		"auth_mode":    upstream.AuthMode,
		"test_url":     testURL,
		"client_spoof": clientSpoof,
	}
	if spoofClaude || spoofCodex || req.Protocol == "anthropic" {
		rh := map[string]string{}
		for _, k := range []string{
			"Anthropic-Version",
			"Anthropic-Beta",
			"User-Agent",
			"X-App",
			"Anthropic-Dangerous-Direct-Browser-Access",
			"X-Claude-Code-Session-Id",
			"x-client-request-id",
			"X-Stainless-Arch",
			"X-Stainless-Lang",
			"X-Stainless-OS",
			"X-Stainless-Package-Version",
			"X-Stainless-Runtime",
			"X-Stainless-Runtime-Version",
			"X-Stainless-Timeout",
			"X-Stainless-Retry-Count",
			"Originator",
			"OpenAI-Beta",
			"Session-Id",
			"Thread-Id",
			"X-Client-Request-Id",
			"X-Codex-Beta-Features",
			"X-Codex-Window-Id",
			"X-Codex-Turn-Metadata",
			"Accept",
			"Accept-Language",
			"Content-Type",
		} {
			if v := httpReq.Header.Get(k); v != "" {
				rh[k] = v
			}
		}
		result["request_headers"] = rh
		if spoofClaude && claudeIdentity.SessionID != "" {
			result["test_session_id"] = claudeIdentity.SessionID
			result["test_device_id"] = claudeIdentity.DeviceID
			result["spoof_client"] = "claude_code"
		}
		if spoofCodex && codexIdentity.SessionID != "" {
			result["test_session_id"] = codexIdentity.SessionID
			result["test_installation_id"] = codexIdentity.InstallationID
			result["test_turn_id"] = codexIdentity.TurnID
			result["spoof_client"] = "codex"
		}
	}
	if !success {
		result["error"] = fmt.Sprintf("上游返回 HTTP %d", resp.StatusCode)
		// 原始响应体（管理面板直接展示，便于排查 OAuth/鉴权等非标准错误结构）
		if len(respBody) > 0 {
			result["raw_body"] = string(respBody)
		}
		// 尝试解析常见错误字段
		var errResp struct {
			Error interface{} `json:"error"`
			// Anthropic 有时用 type/message 顶层字段
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(respBody, &errResp) == nil {
			switch e := errResp.Error.(type) {
			case string:
				if e != "" {
					result["error_message"] = e
				}
			case map[string]interface{}:
				if msg, ok := e["message"].(string); ok && msg != "" {
					result["error_message"] = msg
				} else if t, ok := e["type"].(string); ok && t != "" {
					result["error_message"] = t
				}
			}
			if result["error_message"] == nil {
				if errResp.Message != "" {
					result["error_message"] = errResp.Message
				} else if errResp.Type != "" {
					result["error_message"] = errResp.Type
				}
			}
		}
		if spoofClaude {
			result["hint"] = "已开启 Claude Code 客户端伪装（仅管理面板测试）。若仍 429，上游可能只放行真实 CC 会话；可关掉「客户端伪装」对比，或以请求日志 200 为准。"
		} else if spoofCodex {
			result["hint"] = "已开启 Codex 客户端伪装（仅管理面板测试）。session/originator 均为随机生成，不影响真实 Codex 透传。"
		} else if oauthAnthropic && !clientSpoof {
			result["hint"] = "OAuth 上游未使用客户端伪装（裸 Bearer 探测）。若 429，可打开「客户端伪装」再试。"
		}
	} else {
		// 尝试提取回复内容
		switch req.Protocol {
		case "anthropic":
			reply, actualModel := proxy.ParseAnthropicStreamReply(respBody)
			if reply != "" {
				result["reply"] = reply
			}
			if actualModel != "" {
				result["actual_model"] = actualModel
			}
		case "responses":
			var responsesResp struct {
				Output []struct {
					Type    string `json:"type"`
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"output"`
				Model string `json:"model"`
			}
			if json.Unmarshal(respBody, &responsesResp) == nil {
				for _, item := range responsesResp.Output {
					if item.Type == "message" {
						for _, c := range item.Content {
							if c.Type == "output_text" && c.Text != "" {
								result["reply"] = c.Text
								break
							}
						}
					}
				}
				result["actual_model"] = responsesResp.Model
			}
		default: // openai
			var openaiResp struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
				Model string `json:"model"`
			}
			if json.Unmarshal(respBody, &openaiResp) == nil {
				if len(openaiResp.Choices) > 0 {
					result["reply"] = openaiResp.Choices[0].Message.Content
				}
				result["actual_model"] = openaiResp.Model
			}
		}
	}
	jsonOK(w, result)
}
