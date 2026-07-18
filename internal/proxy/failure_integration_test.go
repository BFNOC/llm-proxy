package proxy

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// classifyUpstreamFailure additional coverage
// ---------------------------------------------------------------------------

func TestClassifyUpstreamFailure_AllStatuses(t *testing.T) {
	tests := []struct {
		status int
		want   FailureKind
	}{
		{401, FailureAuth},
		{403, FailureAuth},
		{502, FailureServerError},
		{503, FailureServerError},
		{504, FailureServerError},
		{200, FailureNone},
		{400, FailureNone},
		{500, FailureNone},
		{404, FailureNone},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("status_%d", tc.status), func(t *testing.T) {
			got := classifyUpstreamFailure(tc.status, nil, nil)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestClassifyUpstreamFailure_429_TransientRateLimit(t *testing.T) {
	// With Retry-After + body that doesn't mention quota -> rate limit.
	h := http.Header{}
	h.Set("Retry-After", "60")
	kind := classifyUpstreamFailure(429, h, []byte(`{"error":{"message":"Too many requests"}}`))
	assert.Equal(t, FailureRateLimit, kind)
}

func TestClassifyUpstreamFailure_429_QuotaByStructuredCode(t *testing.T) {
	body := []byte(`{"error":{"code":"insufficient_quota","message":"You have exhausted your quota"}}`)
	kind := classifyUpstreamFailure(429, http.Header{}, body)
	assert.Equal(t, FailureQuota, kind)
}

func TestClassifyUpstreamFailure_429_QuotaByType(t *testing.T) {
	body := []byte(`{"error":{"type":"insufficient_billing","message":"Please check your plan"}}`)
	kind := classifyUpstreamFailure(429, http.Header{}, body)
	assert.Equal(t, FailureQuota, kind)
}

func TestClassifyUpstreamFailure_NetworkError(t *testing.T) {
	// classifyUpstreamFailure only inspects status/headers/body.
	// Network errors are handled by ServeHTTP before classification.
	// This tests the edge case of status 0 (not a standard HTTP status).
	kind := classifyUpstreamFailure(0, nil, nil)
	assert.Equal(t, FailureNone, kind)
}

// ---------------------------------------------------------------------------
// isQuotaExhausted additional coverage
// ---------------------------------------------------------------------------

func TestIsQuotaExhausted_VariousPatterns(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"insufficient_quota keyword", `{"error":"insufficient_quota"}`, true},
		{"exceeded your current quota", `your account has exceeded your current quota`, true},
		{"quota exceeded", `Error: quota exceeded for this key`, true},
		{"billing keyword", `billing issue detected`, true},
		{"credit balance", `Your credit balance is too low`, true},
		{"payment required", `payment required to continue`, true},
		{"spend limit", `spend limit reached`, true},
		{"usage limit", `usage limit exceeded`, true},
		{"normal rate limit message", `Too many requests, please try again later`, false},
		{"empty body no headers no retry", ``, true}, // no headers, no body -> lean quota
		{"unrelated error", `{"error":"invalid model"}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var hdr http.Header
			if tc.body == "" {
				hdr = nil // nil header
			} else {
				hdr = http.Header{}
			}
			got := isQuotaExhausted(hdr, []byte(tc.body))
			assert.Equal(t, tc.want, got, "body=%q", tc.body)
		})
	}
}

func TestIsQuotaExhausted_WithRetryAfterAndBody(t *testing.T) {
	// Retry-After present but body mentions quota -> still quota.
	h := http.Header{}
	h.Set("Retry-After", "30")
	body := []byte(`{"error":{"code":"insufficient_quota","message":"quota exhausted"}}`)
	assert.True(t, isQuotaExhausted(h, body))
}

func TestIsQuotaExhausted_WithRateLimitHeaders_NoQuotaBody(t *testing.T) {
	h := http.Header{}
	h.Set("x-ratelimit-remaining-requests", "0")
	body := []byte(`{"error":{"message":"rate limited"}}`)
	assert.False(t, isQuotaExhausted(h, body), "rate limit headers + non-quota body = not quota")
}

func TestIsQuotaExhausted_EmptyBodyWithRetryAfter(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "5")
	// Empty body + has Retry-After -> not quota.
	assert.False(t, isQuotaExhausted(h, nil))
}

func TestIsQuotaExhausted_EmptyBodyWithRemainingHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("X-RateLimit-Remaining", "10")
	// Empty body + has rate limit remaining header -> not quota.
	assert.False(t, isQuotaExhausted(h, nil))
}

func TestIsQuotaExhausted_StructuredQuotaCode(t *testing.T) {
	body := []byte(`{"error":{"code":"some_quota_error","type":"normal","message":"something"}}`)
	assert.True(t, isQuotaExhausted(http.Header{}, body))
}

// ---------------------------------------------------------------------------
// peekResponseBody additional coverage
// ---------------------------------------------------------------------------

func TestPeekResponseBody_LargeBody_Truncation(t *testing.T) {
	longData := strings.Repeat("x", 1000)
	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader(longData)),
	}
	got := peekResponseBody(resp, 100)
	assert.Len(t, got, 100, "peek should truncate to limit")

	// Remaining data should still be readable.
	remaining, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Len(t, remaining, 1000, "full content should be available after peek (peeked + remainder)")
}

func TestPeekResponseBody_EmptyBody(t *testing.T) {
	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader("")),
	}
	got := peekResponseBody(resp, 100)
	assert.Empty(t, got)
}

func TestPeekResponseBody_NilResponse(t *testing.T) {
	assert.Nil(t, peekResponseBody(nil, 100))
}

func TestPeekResponseBody_NilBody(t *testing.T) {
	resp := &http.Response{Body: nil}
	assert.Nil(t, peekResponseBody(resp, 100))
}

func TestPeekResponseBody_ZeroLimit(t *testing.T) {
	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader("data")),
	}
	got := peekResponseBody(resp, 0)
	assert.Nil(t, got, "zero limit should return nil")
}

func TestPeekResponseBody_NegativeLimit(t *testing.T) {
	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader("data")),
	}
	got := peekResponseBody(resp, -1)
	assert.Nil(t, got, "negative limit should return nil")
}

func TestPeekResponseBody_ExactSize(t *testing.T) {
	data := "hello"
	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader(data)),
	}
	got := peekResponseBody(resp, int64(len(data)))
	assert.Equal(t, data, string(got))

	// Full content should be re-readable.
	all, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, data, string(all))
}

// ---------------------------------------------------------------------------
// stripRequestHopByHop additional coverage
// ---------------------------------------------------------------------------

func TestStripRequestHopByHop_NilHeader(t *testing.T) {
	// Should not panic.
	stripRequestHopByHop(nil)
}

func TestStripRequestHopByHop_NoConnectionHeader(t *testing.T) {
	h := http.Header{}
	h.Set("Proxy-Authenticate", "Basic")
	h.Set("Upgrade", "websocket")
	h.Set("Content-Type", "application/json")

	stripRequestHopByHop(h)

	assert.Empty(t, h.Get("Proxy-Authenticate"))
	assert.Empty(t, h.Get("Upgrade"))
	assert.Equal(t, "application/json", h.Get("Content-Type"))
}

func TestStripRequestHopByHop_AllStandardHopByHop(t *testing.T) {
	h := http.Header{}
	for name := range hopByHopHeaders {
		h.Set(name, "value")
	}
	h.Set("X-Custom", "keep")

	stripRequestHopByHop(h)

	for name := range hopByHopHeaders {
		assert.Empty(t, h.Get(name), "hop-by-hop header %s should be removed", name)
	}
	assert.Equal(t, "keep", h.Get("X-Custom"))
}

func TestStripRequestHopByHop_ConnectionWithMultipleTokens(t *testing.T) {
	h := http.Header{}
	h.Set("Connection", "X-A, X-B, X-C")
	h.Set("X-A", "a")
	h.Set("X-B", "b")
	h.Set("X-C", "c")
	h.Set("X-D", "keep")

	stripRequestHopByHop(h)

	assert.Empty(t, h.Get("Connection"))
	assert.Empty(t, h.Get("X-A"))
	assert.Empty(t, h.Get("X-B"))
	assert.Empty(t, h.Get("X-C"))
	assert.Equal(t, "keep", h.Get("X-D"))
}
