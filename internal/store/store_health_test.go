package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Health History（上游健康探测历史）
// ---------------------------------------------------------------------------

func TestRecordHealthProbe(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"k"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	// 记录一次健康探测
	err = s.RecordHealthProbe(up.ID, true, 42, "")
	require.NoError(t, err)

	// 记录一次不健康探测
	err = s.RecordHealthProbe(up.ID, false, 0, "connection refused")
	require.NoError(t, err)

	// 查询验证两条记录都已插入
	records, err := s.GetHealthHistory(up.ID, 1, 0)
	require.NoError(t, err)
	assert.Len(t, records, 2)

	// 最新的在前（DESC 排序），不健康的应该是最后插入的
	assert.False(t, records[0].Healthy)
	assert.Equal(t, "connection refused", records[0].ErrorMessage)
	assert.True(t, records[1].Healthy)
	assert.Equal(t, int64(42), records[1].LatencyMs)
}

func TestGetHealthHistory(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"k"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	// 插入多条探测记录
	for i := 0; i < 5; i++ {
		err = s.RecordHealthProbe(up.ID, i%2 == 0, int64(i*10), "")
		require.NoError(t, err)
	}

	// 查询全部（hours=24，limit=0 表示不限制）
	all, err := s.GetHealthHistory(up.ID, 24, 0)
	require.NoError(t, err)
	assert.Len(t, all, 5)

	// 验证 DESC 排序：第一条的 created_at 应大于等于最后一条
	assert.True(t, !all[0].CreatedAt.Before(all[len(all)-1].CreatedAt),
		"结果应按 created_at DESC 排序")

	// 验证 limit 生效
	limited, err := s.GetHealthHistory(up.ID, 24, 3)
	require.NoError(t, err)
	assert.Len(t, limited, 3)
}

func TestGetHealthHistory_EmptyResult(t *testing.T) {
	s := newTestStore(t)

	// 查询不存在的上游 ID，应返回空切片
	records, err := s.GetHealthHistory(99999, 24, 0)
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestCleanHealthHistory(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"k"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	// 手动插入一条"旧"记录（10 天前）
	oldTime := time.Now().UTC().Add(-10 * 24 * time.Hour)
	_, err = s.db.Exec(
		`INSERT INTO upstream_health_history (upstream_id, healthy, latency_ms, error_message, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		up.ID, true, 50, "", oldTime,
	)
	require.NoError(t, err)

	// 插入一条"新"记录（当前时间）
	err = s.RecordHealthProbe(up.ID, true, 30, "")
	require.NoError(t, err)

	// 清理保留 3 天以内的记录，旧的应被删除
	err = s.CleanHealthHistory(3)
	require.NoError(t, err)

	records, err := s.GetHealthHistory(up.ID, 24*365, 0)
	require.NoError(t, err)
	assert.Len(t, records, 1, "只有新记录应保留")
	assert.Equal(t, int64(30), records[0].LatencyMs)
}

func TestCleanHealthHistory_InvalidRetention(t *testing.T) {
	s := newTestStore(t)

	// retentionDays=0 应返回错误
	err := s.CleanHealthHistory(0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "retentionDays")
}

// ---------------------------------------------------------------------------
// Latency Stats（上游延迟统计）
// ---------------------------------------------------------------------------

func TestGetUpstreamLatencyStats(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("lat-key", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	logs := []RequestLog{
		{DownstreamKeyID: dk.ID, UpstreamName: "openai", ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 100, CreatedAt: now},
		{DownstreamKeyID: dk.ID, UpstreamName: "openai", ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 200, CreatedAt: now},
		{DownstreamKeyID: dk.ID, UpstreamName: "openai", ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 300, CreatedAt: now},
		{DownstreamKeyID: dk.ID, UpstreamName: "anthropic", ProviderStyle: "anthropic", Path: "/v1/messages", StatusCode: 200, LatencyMs: 50, CreatedAt: now},
		{DownstreamKeyID: dk.ID, UpstreamName: "anthropic", ProviderStyle: "anthropic", Path: "/v1/messages", StatusCode: 200, LatencyMs: 150, CreatedAt: now},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	stats, err := s.GetUpstreamLatencyStats(24)
	require.NoError(t, err)
	require.Len(t, stats, 2)

	// 构建按名称索引的 map 方便断言
	byName := make(map[string]map[string]interface{})
	for _, st := range stats {
		byName[st["upstream_name"].(string)] = st
	}

	// 验证 openai：avg=200, min=100, max=300
	openaiStats := byName["openai"]
	require.NotNil(t, openaiStats)
	assert.Equal(t, 3, openaiStats["total"])
	assert.InDelta(t, 200.0, openaiStats["avg_latency"].(float64), 0.1)
	assert.InDelta(t, 100.0, openaiStats["min_latency"].(float64), 0.1)
	assert.InDelta(t, 300.0, openaiStats["max_latency"].(float64), 0.1)

	// 验证 anthropic：avg=100, min=50, max=150
	anthropicStats := byName["anthropic"]
	require.NotNil(t, anthropicStats)
	assert.Equal(t, 2, anthropicStats["total"])
	assert.InDelta(t, 100.0, anthropicStats["avg_latency"].(float64), 0.1)
	assert.InDelta(t, 50.0, anthropicStats["min_latency"].(float64), 0.1)
	assert.InDelta(t, 150.0, anthropicStats["max_latency"].(float64), 0.1)
}

func TestGetUpstreamLatencyStats_NoLogs(t *testing.T) {
	s := newTestStore(t)

	stats, err := s.GetUpstreamLatencyStats(24)
	require.NoError(t, err)
	assert.Empty(t, stats)
}
