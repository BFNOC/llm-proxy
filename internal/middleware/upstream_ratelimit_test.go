package middleware

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// 上游 RPM 限流器单元测试
// ---------------------------------------------------------------------------

// TestUpstreamRPMLimiter_Allow 在限制内应允许，超过限制后应拒绝
func TestUpstreamRPMLimiter_Allow(t *testing.T) {
	l := NewUpstreamRPMLimiter()
	defer l.StopGC()

	limit := 3
	upstreamID := int64(1)

	// 前 3 次应允许
	for i := 0; i < limit; i++ {
		assert.True(t, l.Allow(upstreamID, limit), "第 %d 次请求应被允许", i+1)
	}

	// 第 4 次应拒绝
	assert.False(t, l.Allow(upstreamID, limit), "超过限制后应被拒绝")
}

// TestUpstreamRPMLimiter_ZeroUnlimited limit 为 0 时不限制，始终允许
func TestUpstreamRPMLimiter_ZeroUnlimited(t *testing.T) {
	l := NewUpstreamRPMLimiter()
	defer l.StopGC()

	upstreamID := int64(1)

	// limit=0 表示不限制
	for i := 0; i < 100; i++ {
		assert.True(t, l.Allow(upstreamID, 0), "limit=0 时应始终允许")
	}
}

// TestUpstreamRPMLimiter_GetRPM 验证 GetRPM 返回当前分钟内的请求数
func TestUpstreamRPMLimiter_GetRPM(t *testing.T) {
	l := NewUpstreamRPMLimiter()
	defer l.StopGC()

	upstreamID := int64(42)

	// 初始应为 0
	assert.Equal(t, 0, l.GetRPM(upstreamID), "初始 RPM 应为 0")

	// 发送 5 个请求（limit 足够大不会拒绝）
	for i := 0; i < 5; i++ {
		l.Allow(upstreamID, 1000)
	}

	assert.Equal(t, 5, l.GetRPM(upstreamID), "发送 5 个请求后 RPM 应为 5")
}
