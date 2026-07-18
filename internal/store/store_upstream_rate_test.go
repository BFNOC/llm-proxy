package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 上游速率信息
// ---------------------------------------------------------------------------

// TestUpsertUpstreamRateInfo 插入或更新速率信息后可正确读回
func TestUpsertUpstreamRateInfo(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("rate-info", "https://a.example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	info := &UpstreamRateInfo{
		UpstreamID:   up.ID,
		RPMLimit:     100,
		RPMRemaining: 80,
		TPMLimit:     50000,
		TPMRemaining: 40000,
		ResetAt:      &now,
		UpdatedAt:    now,
	}
	err = s.UpsertUpstreamRateInfo(info)
	require.NoError(t, err)

	got, err := s.GetUpstreamRateInfo(up.ID)
	require.NoError(t, err)
	assert.Equal(t, up.ID, got.UpstreamID)
	assert.Equal(t, 100, got.RPMLimit)
	assert.Equal(t, 80, got.RPMRemaining)
	assert.Equal(t, 50000, got.TPMLimit)
	assert.Equal(t, 40000, got.TPMRemaining)
	assert.NotNil(t, got.ResetAt)
	assert.Equal(t, 0, got.Consecutive429s)

	// 再次 upsert 更新值
	info.RPMRemaining = 60
	err = s.UpsertUpstreamRateInfo(info)
	require.NoError(t, err)

	got2, err := s.GetUpstreamRateInfo(up.ID)
	require.NoError(t, err)
	assert.Equal(t, 60, got2.RPMRemaining, "upsert 应更新已有记录")
}

// TestRecord429 记录 429 事件后计数器应递增
func TestRecord429(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("rate-429", "https://a.example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.Record429(up.ID)
	require.NoError(t, err)

	info, err := s.GetUpstreamRateInfo(up.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, info.Consecutive429s)
	assert.NotNil(t, info.Last429At)

	// 再次记录
	err = s.Record429(up.ID)
	require.NoError(t, err)

	info2, err := s.GetUpstreamRateInfo(up.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, info2.Consecutive429s)
}

// TestReset429Counter 记录 429 后重置，计数器应归零
func TestReset429Counter(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("rate-reset", "https://a.example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	// 先记录几次 429
	require.NoError(t, s.Record429(up.ID))
	require.NoError(t, s.Record429(up.ID))
	require.NoError(t, s.Record429(up.ID))

	info, err := s.GetUpstreamRateInfo(up.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, info.Consecutive429s)

	// 重置
	err = s.Reset429Counter(up.ID)
	require.NoError(t, err)

	info2, err := s.GetUpstreamRateInfo(up.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, info2.Consecutive429s)
}

// ---------------------------------------------------------------------------
// 上游配置（RPM 限制 / 熔断器配置）
// ---------------------------------------------------------------------------

// TestSetUpstreamRPMLimit 设置上游 RPM 限制后可通过 GetUpstream 读回
func TestSetUpstreamRPMLimit(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("rpm-limit", "https://a.example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, up.UpstreamRPMLimit, "新建时应为 0")

	err = s.SetUpstreamRPMLimit(up.ID, 500)
	require.NoError(t, err)

	got, err := s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.Equal(t, 500, got.UpstreamRPMLimit)
}

// TestSetCircuitBreakerConfig 设置熔断器配置后可通过 GetUpstream 读回
func TestSetCircuitBreakerConfig(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("cb-config", "https://a.example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, up.CircuitBreakerThreshold)
	assert.Equal(t, 0, up.CircuitBreakerRecoverySeconds)

	err = s.SetCircuitBreakerConfig(up.ID, 5, 30)
	require.NoError(t, err)

	got, err := s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.Equal(t, 5, got.CircuitBreakerThreshold)
	assert.Equal(t, 30, got.CircuitBreakerRecoverySeconds)
}
