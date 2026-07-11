package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// FailureKind classifies an upstream error for failover and key auto-disable.
type FailureKind string

const (
	FailureNone        FailureKind = ""
	FailureAuth        FailureKind = "auth"         // 401/403
	FailureRateLimit   FailureKind = "rate_limit"   // transient 429
	FailureQuota       FailureKind = "quota"        // billing / insufficient_quota 429
	FailureServerError FailureKind = "server_error" // 502/503/504
)

// shouldFailoverStatus reports whether a non-final upstream response should
// trigger trying the next upstream.
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

// shouldCountKeyFailure reports whether this failure should increment
// consecutive_failures (and potentially auto-disable the key).
// Transient server errors do not burn keys; auth/quota/rate-limit do.
func shouldCountKeyFailure(kind FailureKind) bool {
	switch kind {
	case FailureAuth, FailureRateLimit, FailureQuota:
		return true
	default:
		return false
	}
}

// classifyUpstreamFailure inspects status, headers, and optional body snippet.
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

// isQuotaExhausted detects account/billing exhaustion vs temporary rate limits.
func isQuotaExhausted(hdr http.Header, body []byte) bool {
	// OpenAI often omits Retry-After / rate-limit headers on billing 429s.
	// Presence of Retry-After or remaining-rate headers suggests transient RL.
	if hdr != nil {
		if strings.TrimSpace(hdr.Get("Retry-After")) != "" {
			// Still check body — some providers send both.
		}
		if hdr.Get("x-ratelimit-remaining-requests") != "" ||
			hdr.Get("x-ratelimit-remaining-tokens") != "" ||
			hdr.Get("X-RateLimit-Remaining") != "" {
			// Has rate-limit window metadata → treat as rate limit unless body says quota.
		}
	}
	if len(body) == 0 {
		// No body: if no Retry-After and no rate-limit remaining headers, lean quota.
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
	// Structured OpenAI error code
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

// peekResponseBody reads up to limit bytes for classification, then restores
// resp.Body so the original Closer is preserved (avoids connection leaks).
func peekResponseBody(resp *http.Response, limit int64) []byte {
	if resp == nil || resp.Body == nil || limit <= 0 {
		return nil
	}
	limited := io.LimitReader(resp.Body, limit)
	buf, err := io.ReadAll(limited)
	if err != nil && len(buf) == 0 {
		return nil
	}
	// Restore: peeked prefix + unread remainder, keep original Closer.
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

// stripRequestHopByHop removes hop-by-hop headers from an outbound request,
// including names listed in the Connection header (RFC 7230).
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
