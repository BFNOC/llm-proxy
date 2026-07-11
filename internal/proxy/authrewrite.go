package proxy

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"strings"
)

// AuthMode values for Anthropic credential header selection.
const (
	AuthModeAPIKey = "api_key" // x-api-key (default)
	AuthModeOAuth  = "oauth"   // Authorization: Bearer (Claude OAuth / sk-ant-oat|oai)
)

// Anthropic OAuth / Claude Code client fingerprints.
//
// Captured from real Claude Code → llm-proxy traffic (claude-cli/2.1.201).
// When the upstream uses an OAuth subscription token (sk-ant-oat*), a bare
// Authorization: Bearer + anthropic-version often lands outside the Claude
// Code rate-limit pool and returns opaque HTTP 429 rate_limit_error.
//
// Note: inbound Claude Code requests typically do NOT include oauth-2025-04-20
// (they authenticate to the proxy with a downstream key). That beta is merged
// only when we attach an OAuth upstream token.
const (
	// Keep in sync with `claude --version` when possible.
	AnthropicOAuthUserAgent = "claude-cli/2.1.201 (external, cli)"
	AnthropicOAuthXApp      = "cli"

	AnthropicOAuthBetaOAuth      = "oauth-2025-04-20"
	AnthropicOAuthBetaClaudeCode = "claude-code-20250219"

	// Stainless client fingerprints from Claude Code (Node SDK).
	AnthropicStainlessArch           = "arm64"
	AnthropicStainlessLang           = "js"
	AnthropicStainlessOS             = "MacOS"
	AnthropicStainlessPackageVersion = "0.94.0"
	AnthropicStainlessRuntime        = "node"
	AnthropicStainlessRuntimeVersion = "v24.18.0"
	AnthropicStainlessTimeout        = "600"
)

// anthropicClaudeCodeBetas is the anthropic-beta list observed on real
// Claude Code Messages requests (order preserved from capture).
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

// RewriteAuthHeaders rewrites authentication headers on the outgoing request so
// that the upstream provider receives the correct credential format.
//
// OpenAI style:
//   - Sets "Authorization: Bearer {upstreamKey}"
//   - Removes "x-api-key"
//
// Anthropic style (authMode=api_key, default):
//   - Sets "x-api-key: {upstreamKey}"
//   - Removes "Authorization"
//
// Anthropic style (authMode=oauth):
//   - Sets "Authorization: Bearer {upstreamKey}"
//   - Removes "x-api-key"
//   - Ensures Claude Code client fingerprints (+ oauth beta for subscription tokens)
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
	default: // StyleOpenAI
		r.Header.Set("Authorization", "Bearer "+upstreamKey)
		r.Header.Del("x-api-key")
	}
}

// EnsureAnthropicOAuthClientHeaders merges Claude Code client fingerprints.
// Existing header values from a real client are preserved; only missing pieces
// are filled. Always ensures oauth-2025-04-20 is present for OAuth upstream keys.
func EnsureAnthropicOAuthClientHeaders(h http.Header) {
	if h == nil {
		return
	}

	// Beta list: keep client-provided tokens, ensure Claude Code set + oauth beta.
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

	// Stainless fingerprints (only fill if missing so real CC values win).
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
	// UUID v4 bits
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
