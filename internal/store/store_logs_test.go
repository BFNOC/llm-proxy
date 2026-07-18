package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Request Logs
// ---------------------------------------------------------------------------

func TestRequestLog_InsertBatchAndQuery(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("log-key", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	logs := []RequestLog{
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/chat/completions", StatusCode: 200, LatencyMs: 150, CreatedAt: now},
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/chat/completions", StatusCode: 429, LatencyMs: 10, CreatedAt: now.Add(-time.Minute)},
	}

	err = s.InsertRequestLogBatch(logs)
	require.NoError(t, err)

	results, err := s.QueryLogs(dk.ID, now.Add(-time.Hour), now.Add(time.Hour), 100)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestRequestLog_InsertBatch_Empty(t *testing.T) {
	s := newTestStore(t)
	err := s.InsertRequestLogBatch(nil)
	assert.NoError(t, err)

	err = s.InsertRequestLogBatch([]RequestLog{})
	assert.NoError(t, err)
}

func TestRequestLog_QueryLogs_AllKeys(t *testing.T) {
	s := newTestStore(t)

	_, dk1, err := s.CreateKey("key-1", 0)
	require.NoError(t, err)
	_, dk2, err := s.CreateKey("key-2", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	logs := []RequestLog{
		{DownstreamKeyID: dk1.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 50, CreatedAt: now},
		{DownstreamKeyID: dk2.ID, ProviderStyle: "anthropic", Path: "/v1/messages", StatusCode: 200, LatencyMs: 80, CreatedAt: now},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	// keyID=0 means all keys
	results, err := s.QueryLogs(0, now.Add(-time.Hour), now.Add(time.Hour), 0)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestRequestLog_QueryLogs_FilterByKey(t *testing.T) {
	s := newTestStore(t)

	_, dk1, err := s.CreateKey("key-a", 0)
	require.NoError(t, err)
	_, dk2, err := s.CreateKey("key-b", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	logs := []RequestLog{
		{DownstreamKeyID: dk1.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 50, CreatedAt: now},
		{DownstreamKeyID: dk2.ID, ProviderStyle: "anthropic", Path: "/v1/messages", StatusCode: 200, LatencyMs: 80, CreatedAt: now},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	results, err := s.QueryLogs(dk1.ID, now.Add(-time.Hour), now.Add(time.Hour), 0)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, dk1.ID, results[0].DownstreamKeyID)
}

func TestRequestLog_QueryLogs_Limit(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("limit-key", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	logs := make([]RequestLog, 5)
	for i := range logs {
		logs[i] = RequestLog{
			DownstreamKeyID: dk.ID,
			ProviderStyle:   "openai",
			Path:            "/v1/chat",
			StatusCode:      200,
			LatencyMs:       int64(i * 10),
			CreatedAt:       now.Add(time.Duration(i) * time.Second),
		}
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	results, err := s.QueryLogs(dk.ID, now.Add(-time.Hour), now.Add(time.Hour), 3)
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestDeleteLogsOlderThan(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("old-log-key", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-30 * time.Minute)

	logs := []RequestLog{
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 10, CreatedAt: old},
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 10, CreatedAt: recent},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	// Delete logs older than 24 hours — should remove the 48h-old entry
	err = s.DeleteLogsOlderThan(24 * time.Hour)
	require.NoError(t, err)

	results, err := s.QueryLogs(dk.ID, now.Add(-72*time.Hour), now.Add(time.Hour), 0)
	require.NoError(t, err)
	assert.Len(t, results, 1, "only the recent log should remain")
	assert.Equal(t, recent.Unix(), results[0].CreatedAt.Unix())
}

// ---------------------------------------------------------------------------
// CountLogsSince
// ---------------------------------------------------------------------------

func TestCountLogsSince_Empty(t *testing.T) {
	s := newTestStore(t)
	count, err := s.CountLogsSince(time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCountLogsSince_InclusiveBoundary(t *testing.T) {
	s := newTestStore(t)
	_, dk, _ := s.CreateKey("count-key", 0)

	exact := time.Now().UTC().Truncate(time.Second)
	logs := []RequestLog{
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 10, CreatedAt: exact},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	count, err := s.CountLogsSince(exact)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "log at exact boundary should be counted")
}

func TestCountLogsSince_MixedOldAndRecent(t *testing.T) {
	s := newTestStore(t)
	_, dk, _ := s.CreateKey("mixed-key", 0)

	now := time.Now().UTC()
	logs := []RequestLog{
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/a", StatusCode: 200, LatencyMs: 10, CreatedAt: now.Add(-2 * time.Hour)},
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/b", StatusCode: 200, LatencyMs: 10, CreatedAt: now.Add(-30 * time.Minute)},
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/c", StatusCode: 200, LatencyMs: 10, CreatedAt: now},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	count, err := s.CountLogsSince(now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, count, "should count only logs within last hour")
}

// ---------------------------------------------------------------------------
// GetKeyUsageStats
// ---------------------------------------------------------------------------

func TestGetKeyUsageStats_Empty(t *testing.T) {
	s := newTestStore(t)
	stats, err := s.GetKeyUsageStats()
	require.NoError(t, err)
	assert.Empty(t, stats)
}

func TestGetKeyUsageStats_Aggregation(t *testing.T) {
	s := newTestStore(t)
	_, dk1, err := s.CreateKey("stats-key-1", 0)
	require.NoError(t, err)
	_, dk2, err := s.CreateKey("stats-key-2", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	logs := []RequestLog{
		// dk1: 2 success (200), 1 error (500)
		{DownstreamKeyID: dk1.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 100, CreatedAt: now},
		{DownstreamKeyID: dk1.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 200, CreatedAt: now},
		{DownstreamKeyID: dk1.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 500, LatencyMs: 300, CreatedAt: now},
		// dk2: 1 success
		{DownstreamKeyID: dk2.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 50, CreatedAt: now},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	stats, err := s.GetKeyUsageStats()
	require.NoError(t, err)
	require.Len(t, stats, 2)

	// Results ordered by total DESC, so dk1 (3 total) comes first
	assert.Equal(t, dk1.ID, stats[0].KeyID)
	assert.Equal(t, 3, stats[0].Total)
	assert.Equal(t, 2, stats[0].Success)
	assert.Equal(t, 1, stats[0].Error)
	assert.InDelta(t, 200.0, stats[0].AvgLatencyMs, 0.1) // (100+200+300)/3

	assert.Equal(t, dk2.ID, stats[1].KeyID)
	assert.Equal(t, 1, stats[1].Total)
	assert.Equal(t, 1, stats[1].Success)
	assert.Equal(t, 0, stats[1].Error)
	assert.InDelta(t, 50.0, stats[1].AvgLatencyMs, 0.1)
}

// ---------------------------------------------------------------------------
// InsertRequestLogBatch (additional coverage for extra fields)
// ---------------------------------------------------------------------------

func TestInsertRequestLogBatch_AllFields(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("log-key", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	logs := []RequestLog{
		{
			DownstreamKeyID: dk.ID,
			UpstreamName:    "openai-prod",
			UpstreamKeyIdx:  2,
			Model:           "gpt-4o",
			UsedProxy:       "http://proxy.example.com:8080",
			ClientIP:        "1.2.3.4",
			IPRegion:        "US",
			ProviderStyle:   "openai",
			Path:            "/v1/chat/completions",
			StatusCode:      200,
			LatencyMs:       150,
			CreatedAt:       now,
		},
	}
	err = s.InsertRequestLogBatch(logs)
	require.NoError(t, err)

	results, err := s.QueryLogs(dk.ID, now.Add(-time.Minute), now.Add(time.Minute), 0)
	require.NoError(t, err)
	require.Len(t, results, 1)

	r := results[0]
	assert.Equal(t, "openai-prod", r.UpstreamName)
	assert.Equal(t, 2, r.UpstreamKeyIdx)
	assert.Equal(t, "gpt-4o", r.Model)
	assert.Equal(t, "http://proxy.example.com:8080", r.UsedProxy)
	assert.Equal(t, "1.2.3.4", r.ClientIP)
	assert.Equal(t, "US", r.IPRegion)
}

func TestInsertRequestLogBatch_ZeroCreatedAt(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("zero-ts-key", 0)
	require.NoError(t, err)

	logs := []RequestLog{
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 10},
		// CreatedAt is zero
	}
	err = s.InsertRequestLogBatch(logs)
	require.NoError(t, err)

	// Should have auto-filled CreatedAt, so query with a wide window should find it
	results, err := s.QueryLogs(dk.ID, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 0)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.False(t, results[0].CreatedAt.IsZero(), "zero CreatedAt should be auto-filled")
}

// ---------------------------------------------------------------------------
// InsertRequestLogBatch — edge cases
// ---------------------------------------------------------------------------

func TestInsertRequestLogBatch_NilIsNoOp(t *testing.T) {
	s := newTestStore(t)
	err := s.InsertRequestLogBatch(nil)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// DeleteLogsOlderThan — edge cases
// ---------------------------------------------------------------------------

func TestDeleteLogsOlderThan_NoMatchingLogs(t *testing.T) {
	s := newTestStore(t)
	_, dk, _ := s.CreateKey("k", 0)

	now := time.Now().UTC()
	logs := []RequestLog{
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 10, CreatedAt: now},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	// Delete logs older than 1 hour — none should match
	err := s.DeleteLogsOlderThan(time.Hour)
	require.NoError(t, err)

	results, err := s.QueryLogs(dk.ID, now.Add(-time.Minute), now.Add(time.Minute), 0)
	require.NoError(t, err)
	assert.Len(t, results, 1, "recent log should still exist")
}

func TestDeleteLogsOlderThan_EmptyDatabase(t *testing.T) {
	s := newTestStore(t)

	// Should be a no-op on empty table, no error
	err := s.DeleteLogsOlderThan(time.Hour)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// RequestLog with sizes（请求/响应大小字段）
// ---------------------------------------------------------------------------

func TestInsertRequestLogBatch_WithSizes(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("size-key", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	logs := []RequestLog{
		{
			DownstreamKeyID: dk.ID,
			ProviderStyle:   "openai",
			Path:            "/v1/chat/completions",
			StatusCode:      200,
			LatencyMs:       100,
			RequestSize:     1024,
			ResponseSize:    4096,
			CreatedAt:       now,
		},
		{
			DownstreamKeyID: dk.ID,
			ProviderStyle:   "anthropic",
			Path:            "/v1/messages",
			StatusCode:      200,
			LatencyMs:       200,
			RequestSize:     512,
			ResponseSize:    8192,
			CreatedAt:       now,
		},
	}
	err = s.InsertRequestLogBatch(logs)
	require.NoError(t, err)

	// 查询并验证 size 字段
	results, err := s.QueryLogs(dk.ID, now.Add(-time.Minute), now.Add(time.Minute), 0)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// 结果按 DESC 排序，但两条 CreatedAt 相同，按插入顺序的反序
	// 遍历检查所有记录的 size 都正确
	sizeMap := make(map[string][2]int64) // path -> {requestSize, responseSize}
	for _, r := range results {
		sizeMap[r.Path] = [2]int64{r.RequestSize, r.ResponseSize}
	}
	assert.Equal(t, [2]int64{1024, 4096}, sizeMap["/v1/chat/completions"])
	assert.Equal(t, [2]int64{512, 8192}, sizeMap["/v1/messages"])
}

func TestQueryLogs_IncludesSizes(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("size-query-key", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	logs := []RequestLog{
		{
			DownstreamKeyID: dk.ID,
			ProviderStyle:   "openai",
			Path:            "/v1/chat",
			StatusCode:      200,
			LatencyMs:       50,
			RequestSize:     256,
			ResponseSize:    2048,
			CreatedAt:       now,
		},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	results, err := s.QueryLogs(dk.ID, now.Add(-time.Minute), now.Add(time.Minute), 0)
	require.NoError(t, err)
	require.Len(t, results, 1)

	r := results[0]
	assert.Equal(t, int64(256), r.RequestSize, "QueryLogs 应返回 request_size")
	assert.Equal(t, int64(2048), r.ResponseSize, "QueryLogs 应返回 response_size")
}

// ---------------------------------------------------------------------------
// GetLogByID
// ---------------------------------------------------------------------------

func TestGetLogByID(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("log-by-id-key", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	logs := []RequestLog{
		{
			DownstreamKeyID: dk.ID,
			UpstreamName:    "test-upstream",
			UpstreamKeyIdx:  1,
			Model:           "gpt-4o",
			UsedProxy:       "http://proxy:8080",
			ClientIP:        "10.0.0.1",
			IPRegion:        "CN",
			ProviderStyle:   "openai",
			Path:            "/v1/chat/completions",
			StatusCode:      200,
			LatencyMs:       123,
			RequestSize:     512,
			ResponseSize:    2048,
			CreatedAt:       now,
		},
	}
	err = s.InsertRequestLogBatch(logs)
	require.NoError(t, err)

	// 先通过 QueryLogs 获取插入的日志 ID
	results, err := s.QueryLogs(dk.ID, now.Add(-time.Minute), now.Add(time.Minute), 1)
	require.NoError(t, err)
	require.Len(t, results, 1)
	logID := results[0].ID

	// 通过 GetLogByID 获取单条日志
	rl, err := s.GetLogByID(logID)
	require.NoError(t, err)
	require.NotNil(t, rl)

	assert.Equal(t, logID, rl.ID)
	assert.Equal(t, dk.ID, rl.DownstreamKeyID)
	assert.Equal(t, "test-upstream", rl.UpstreamName)
	assert.Equal(t, 1, rl.UpstreamKeyIdx)
	assert.Equal(t, "gpt-4o", rl.Model)
	assert.Equal(t, "http://proxy:8080", rl.UsedProxy)
	assert.Equal(t, "10.0.0.1", rl.ClientIP)
	assert.Equal(t, "CN", rl.IPRegion)
	assert.Equal(t, "openai", rl.ProviderStyle)
	assert.Equal(t, "/v1/chat/completions", rl.Path)
	assert.Equal(t, 200, rl.StatusCode)
	assert.Equal(t, int64(123), rl.LatencyMs)
	assert.Equal(t, int64(512), rl.RequestSize)
	assert.Equal(t, int64(2048), rl.ResponseSize)
	assert.Equal(t, now.Unix(), rl.CreatedAt.Unix())
}

func TestGetLogByID_NotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.GetLogByID(99999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
