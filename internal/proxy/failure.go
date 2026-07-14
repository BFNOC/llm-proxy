package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// FailureKind 对上游错误进行分类，用于故障切换和 Key 自动禁用。
type FailureKind string

const (
	FailureNone        FailureKind = ""
	FailureAuth        FailureKind = "auth"         // 401/403
	FailureRateLimit   FailureKind = "rate_limit"   // 临时性 429
	FailureQuota       FailureKind = "quota"        // 账单 / insufficient_quota 429
	FailureServerError FailureKind = "server_error" // 502/503/504
)

// shouldFailoverStatus 报告非最终上游的响应是否应该触发尝试下一个上游。
func shouldFailoverStatus(code int) bool {
	switch code {
	case http.StatusUnauthorized, // 401
		http.StatusForbidden, // 403
		http.StatusTooManyRequests, // 429
		http.StatusBadGateway, // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout: // 504
		return true
	default:
		return false
	}
}

// shouldCountKeyFailure 报告此次失败是否应该使 consecutive_failures 递增
//（并可能触发 Key 自动禁用）。
// 临时性的服务端错误不会消耗 Key 的失败计数；鉴权/额度/限流错误会。
func shouldCountKeyFailure(kind FailureKind) bool {
	switch kind {
	case FailureAuth, FailureRateLimit, FailureQuota:
		return true
	default:
		return false
	}
}

// classifyUpstreamFailure 检查状态码、响应头以及可选的响应体片段。
func classifyUpstreamFailure(status int, hdr http.Header, body []byte) FailureKind {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return FailureAuth
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return FailureServerError
	case http.StatusTooManyRequests:
		if isQuotaExhausted(hdr, body) {
			return FailureQuota
		}
		return FailureRateLimit
	default:
		return FailureNone
	}
}

// isQuotaExhausted 判断是账户/账单额度耗尽，还是临时性限流。
func isQuotaExhausted(hdr http.Header, body []byte) bool {
	// OpenAI 在账单类 429 上经常省略 Retry-After / rate-limit 相关的响应头。
	// 存在 Retry-After 或 remaining-rate 头通常意味着是临时性限流。
	if hdr != nil {
		if strings.TrimSpace(hdr.Get("Retry-After")) != "" {
			// 仍然要检查响应体 —— 有些服务商两者都会发送。
		}
		if hdr.Get("x-ratelimit-remaining-requests") != "" ||
			hdr.Get("x-ratelimit-remaining-tokens") != "" ||
			hdr.Get("X-RateLimit-Remaining") != "" {
			// 存在限流窗口的元数据 → 视为限流，除非响应体明确指出是额度问题。
		}
	}
	if len(body) == 0 {
		// 无响应体：如果既没有 Retry-After 也没有限流剩余量的头，倾向判定为额度问题。
		if hdr == nil {
			return true
		}
		if strings.TrimSpace(hdr.Get("Retry-After")) == "" &&
			hdr.Get("x-ratelimit-remaining-requests") == "" &&
			hdr.Get("x-ratelimit-remaining-tokens") == "" &&
			hdr.Get("X-RateLimit-Remaining") == "" {
			return true
		}
		return false
	}
	lower := strings.ToLower(string(body))
	quotaSignals := []string{
		"insufficient_quota",
		"exceeded your current quota",
		"quota exceeded",
		"billing",
		"credit balance",
		"payment required",
		"spend limit",
		"usage limit",
	}
	for _, s := range quotaSignals {
		if strings.Contains(lower, s) {
			return true
		}
	}
	// 结构化的 OpenAI 错误码
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		code := strings.ToLower(envelope.Error.Code)
		typ := strings.ToLower(envelope.Error.Type)
		if code == "insufficient_quota" || strings.Contains(code, "quota") {
			return true
		}
		if strings.Contains(typ, "insufficient") || strings.Contains(typ, "billing") {
			return true
		}
	}
	return false
}

// peekResponseBody 读取最多 limit 字节用于分类判断，随后恢复 resp.Body，
// 保留原始 Closer（避免连接泄漏）。
func peekResponseBody(resp *http.Response, limit int64) []byte {
	if resp == nil || resp.Body == nil || limit <= 0 {
		return nil
	}
	limited := io.LimitReader(resp.Body, limit)
	buf, err := io.ReadAll(limited)
	if err != nil && len(buf) == 0 {
		return nil
	}
	// 恢复：已窥探的前缀 + 尚未读取的剩余部分，保留原始 Closer。
	orig := resp.Body
	resp.Body = struct {
		io.Reader
		io.Closer
	}{
		Reader: io.MultiReader(bytes.NewReader(buf), orig),
		Closer: orig,
	}
	return buf
}

// stripRequestHopByHop 移除外发请求中的逐跳头，
// 包括 Connection 头中列出的名称（RFC 7230）。
func stripRequestHopByHop(h http.Header) {
	if h == nil {
		return
	}
	if c := h.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			if name := strings.TrimSpace(f); name != "" {
				h.Del(name)
			}
		}
	}
	for name := range hopByHopHeaders {
		h.Del(name)
	}
}
