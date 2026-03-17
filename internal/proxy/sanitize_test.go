package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeErrorBody_TokenIdentifier(t *testing.T) {
	input := `{"error":{"message":"[sk-Mam***nBI] 该令牌额度已用尽"}}`
	want := `{"error":{"message":"[***] 该令牌额度已用尽"}}`
	got := string(SanitizeErrorBody([]byte(input)))
	assert.Equal(t, want, got)
}

func TestSanitizeErrorBody_RequestID(t *testing.T) {
	input := `{"error":{"message":"error occurred (request id: 20260317044318197169144yvydTNy1)"}}`
	want := `{"error":{"message":"error occurred "}}`
	got := string(SanitizeErrorBody([]byte(input)))
	assert.Equal(t, want, got)
}

func TestSanitizeErrorBody_RequestID_CaseInsensitive(t *testing.T) {
	input := `{"error":{"message":"error (Request ID: ABC123)"}}`
	want := `{"error":{"message":"error "}}`
	got := string(SanitizeErrorBody([]byte(input)))
	assert.Equal(t, want, got)
}

func TestSanitizeErrorBody_Quota(t *testing.T) {
	input := `{"error":{"message":"!token.UnlimitedQuota && token.RemainQuota = -90753"}}`
	want := `{"error":{"message":"!token.UnlimitedQuota && token.RemainQuota = ***"}}`
	got := string(SanitizeErrorBody([]byte(input)))
	assert.Equal(t, want, got)
}

func TestSanitizeErrorBody_Combined(t *testing.T) {
	// 模拟真实上游错误消息，同时包含令牌、额度和请求 ID
	input := `{"error":{"message":"[sk-Mam***nBI] 该令牌额度已用尽 !token.UnlimitedQuota && token.RemainQuota = -90753 (request id: 20260317044318197169144yvydTNy1)","type":"new_api_error"}}`
	want := `{"error":{"message":"[***] 该令牌额度已用尽 !token.UnlimitedQuota && token.RemainQuota = *** ","type":"new_api_error"}}`
	got := string(SanitizeErrorBody([]byte(input)))
	assert.Equal(t, want, got)
}

func TestSanitizeErrorBody_NoSensitiveInfo(t *testing.T) {
	// 不含敏感信息的正常错误消息应原样返回
	input := `{"error":{"message":"model not found","type":"invalid_request_error"}}`
	got := string(SanitizeErrorBody([]byte(input)))
	assert.Equal(t, input, got)
}

func TestSanitizeErrorBody_AKStyle_Token(t *testing.T) {
	// 测试 [ak-xxx] 风格的令牌标识也会被脱敏
	input := `{"error":{"message":"[ak-TestKey123] quota exceeded"}}`
	want := `{"error":{"message":"[***] quota exceeded"}}`
	got := string(SanitizeErrorBody([]byte(input)))
	assert.Equal(t, want, got)
}

func TestSanitizeErrorBody_EmptyBody(t *testing.T) {
	got := SanitizeErrorBody([]byte{})
	assert.Empty(t, got)
}
