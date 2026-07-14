package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// ExtractDownstreamKey 获取下游客户端提供的原始 API Key。
//
// OpenAI 风格："Authorization: Bearer {key}"
// Anthropic 风格："x-api-key: {key}"
//
// 没有 Key 时返回空字符串。
func ExtractDownstreamKey(r *http.Request, style ProviderStyle) string {
	switch style {
	case StyleAnthropic:
		if key := r.Header.Get("x-api-key"); key != "" {
			return key
		}
		// 兜底：部分客户端（如 Claude Code）发送 Anthropic 请求时
		// 使用 Authorization: Bearer 而非 x-api-key。
		authHeader := r.Header.Get("Authorization")
		const bearerPrefix = "Bearer "
		if strings.HasPrefix(authHeader, bearerPrefix) {
			return strings.TrimPrefix(authHeader, bearerPrefix)
		}
		return ""
	default: // StyleOpenAI 风格
		authHeader := r.Header.Get("Authorization")
		const bearerPrefix = "Bearer "
		if strings.HasPrefix(authHeader, bearerPrefix) {
			return strings.TrimPrefix(authHeader, bearerPrefix)
		}
		return ""
	}
}

// HashKey 返回给定 Key 的 SHA-256 十六进制摘要。用于在不暴露原始密钥的
// 前提下存储或记录 Key 标识。
func HashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
