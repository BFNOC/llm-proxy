package proxy

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"strings"
)

// AuthMode 取值，用于选择 Anthropic 鉴权凭据所在的请求头。
const (
	AuthModeAPIKey = "api_key" // x-api-key（默认）
	AuthModeOAuth  = "oauth"   // Authorization: Bearer（Claude OAuth / sk-ant-oat|oai）
)

// Anthropic OAuth / Claude Code 客户端指纹。
//
// 采集自真实的 Claude Code → llm-proxy 流量（claude-cli/2.1.201）。
// 当上游使用 OAuth 订阅令牌（sk-ant-oat*）时，裸的
// Authorization: Bearer + anthropic-version 组合往往落在 Claude
// Code 限流池之外，会返回不透明的 HTTP 429 rate_limit_error。
//
// 注意：入站的 Claude Code 请求通常不会带 oauth-2025-04-20
//（它们用下游 Key 向本代理鉴权）。只有在我们附加 OAuth 上游令牌时，
// 才会合并这个 beta 标记。
const (
	// 尽量与 `claude --version` 保持同步。
	AnthropicOAuthUserAgent = "claude-cli/2.1.201 (external, cli)"
	AnthropicOAuthXApp      = "cli"

	AnthropicOAuthBetaOAuth      = "oauth-2025-04-20"
	AnthropicOAuthBetaClaudeCode = "claude-code-20250219"

	// Stainless 客户端指纹，来自 Claude Code（Node SDK）。
	AnthropicStainlessArch           = "arm64"
	AnthropicStainlessLang           = "js"
	AnthropicStainlessOS             = "MacOS"
	AnthropicStainlessPackageVersion = "0.94.0"
	AnthropicStainlessRuntime        = "node"
	AnthropicStainlessRuntimeVersion = "v24.18.0"
	AnthropicStainlessTimeout        = "600"
)

// anthropicClaudeCodeBetas 是从真实 Claude Code Messages 请求中观察到的
// anthropic-beta 列表（顺序保持与抓包一致）。
var anthropicClaudeCodeBetas = []string{
	"claude-code-20250219",
	"context-1m-2025-08-07",
	"interleaved-thinking-2025-05-14",
	"thinking-token-count-2026-05-13",
	"context-management-2025-06-27",
	"prompt-caching-scope-2026-01-05",
	"mid-conversation-system-2026-04-07",
	"advanced-tool-use-2025-11-20",
	"effort-2025-11-24",
	"fallback-credit-2026-06-01",
}

// RewriteAuthHeaders 重写外发请求的鉴权头，使上游服务商收到正确格式的凭据。
//
// OpenAI 风格：
//   - 设置 "Authorization: Bearer {upstreamKey}"
//   - 移除 "x-api-key"
//
// Anthropic 风格（authMode=api_key，默认）：
//   - 设置 "x-api-key: {upstreamKey}"
//   - 移除 "Authorization"
//
// Anthropic 风格（authMode=oauth）：
//   - 设置 "Authorization: Bearer {upstreamKey}"
//   - 移除 "x-api-key"
//   - 确保带上 Claude Code 客户端指纹（订阅令牌还需附加 oauth beta）
func RewriteAuthHeaders(r *http.Request, style ProviderStyle, upstreamKey string, authMode string) {
	if upstreamKey == "" {
		r.Header.Del("Authorization")
		r.Header.Del("x-api-key")
		return
	}
	switch style {
	case StyleAnthropic:
		if authMode == AuthModeOAuth {
			r.Header.Set("Authorization", "Bearer "+upstreamKey)
			r.Header.Del("x-api-key")
			EnsureAnthropicOAuthClientHeaders(r.Header)
		} else {
			r.Header.Set("x-api-key", upstreamKey)
			r.Header.Del("Authorization")
		}
	default: // StyleOpenAI 风格
		r.Header.Set("Authorization", "Bearer "+upstreamKey)
		r.Header.Del("x-api-key")
	}
}

// EnsureAnthropicOAuthClientHeaders 合并 Claude Code 客户端指纹。
// 来自真实客户端的已有头值会被保留，仅补齐缺失的部分。
// 对于 OAuth 上游 Key，始终确保存在 oauth-2025-04-20。
func EnsureAnthropicOAuthClientHeaders(h http.Header) {
	if h == nil {
		return
	}

	// Beta 列表：保留客户端提供的 token，并确保包含 Claude Code 集合 + oauth beta。
	required := make([]string, 0, len(anthropicClaudeCodeBetas)+1)
	required = append(required, AnthropicOAuthBetaOAuth)
	required = append(required, anthropicClaudeCodeBetas...)

	existing := splitCSVHeader(h.Get("anthropic-beta"))
	seen := make(map[string]bool, len(existing)+len(required))
	var out []string
	for _, p := range existing {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, p := range required {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) > 0 {
		h.Set("anthropic-beta", strings.Join(out, ","))
	}

	if h.Get("anthropic-version") == "" {
		h.Set("anthropic-version", "2023-06-01")
	}
	ua := h.Get("User-Agent")
	if ua == "" || strings.HasPrefix(ua, "Go-http-client/") {
		h.Set("User-Agent", AnthropicOAuthUserAgent)
	}
	if h.Get("x-app") == "" {
		h.Set("x-app", AnthropicOAuthXApp)
	}
	if h.Get("anthropic-dangerous-direct-browser-access") == "" {
		h.Set("anthropic-dangerous-direct-browser-access", "true")
	}
	if h.Get("x-claude-code-session-id") == "" {
		h.Set("x-claude-code-session-id", newClaudeCodeSessionID())
	}

	// Stainless 指纹（仅在缺失时填充，确保真实 CC 值优先生效）。
	setIfEmpty(h, "x-stainless-arch", AnthropicStainlessArch)
	setIfEmpty(h, "x-stainless-lang", AnthropicStainlessLang)
	setIfEmpty(h, "x-stainless-os", AnthropicStainlessOS)
	setIfEmpty(h, "x-stainless-package-version", AnthropicStainlessPackageVersion)
	setIfEmpty(h, "x-stainless-runtime", AnthropicStainlessRuntime)
	setIfEmpty(h, "x-stainless-runtime-version", AnthropicStainlessRuntimeVersion)
	setIfEmpty(h, "x-stainless-timeout", AnthropicStainlessTimeout)
	setIfEmpty(h, "x-stainless-retry-count", "0")

	setIfEmpty(h, "accept", "application/json")
	setIfEmpty(h, "accept-language", "*")
}

func setIfEmpty(h http.Header, key, value string) {
	if h.Get(key) == "" {
		h.Set(key, value)
	}
}

func newClaudeCodeSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	// UUID v4 标志位
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func splitCSVHeader(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
