package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// === 请求重放 ===

// TestReplayRequest 通过 store 插入日志后调用 replay，校验返回预填字段
func TestReplayRequest(t *testing.T) {
	h, router := setupTestAdmin(t)

	// 先创建上游（InsertRequestLogBatch 需要 upstream_name 对应到已有记录非必须，但保持真实场景）
	_, err := h.store.CreateUpstream("replay-up", "https://api.openai.com", nil, 1, "", "round-robin", "api_key", "", false, false)
	require.NoError(t, err)

	// 插入一条请求日志
	logs := []store.RequestLog{
		{
			DownstreamKeyID: 0,
			UpstreamName:    "replay-up",
			UpstreamKeyIdx:  0,
			Model:           "gpt-4o",
			Path:            "/v1/chat/completions",
			ProviderStyle:   "openai",
			StatusCode:      200,
			LatencyMs:       100,
			CreatedAt:       time.Now().UTC(),
		},
	}
	err = h.store.InsertRequestLogBatch(logs)
	require.NoError(t, err)

	// 查询日志取 ID（需要传入合理的时间范围）
	from := time.Now().UTC().Add(-1 * time.Minute)
	to := time.Now().UTC().Add(1 * time.Minute)
	dbLogs, err := h.store.QueryLogs(0, from, to, 1)
	require.NoError(t, err)
	require.Len(t, dbLogs, 1)
	logID := dbLogs[0].ID

	// 调用 replay
	urlPath := fmt.Sprintf("/admin/api/logs/%d/replay", logID)
	rr := doRequest(t, router, "POST", urlPath, nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, "replay-up", result["upstream_name"])
	assert.Equal(t, "gpt-4o", result["model"])
	assert.Equal(t, "/v1/chat/completions", result["path"])
	assert.Equal(t, "openai", result["provider_style"])
	assert.Equal(t, float64(logID), result["log_id"])
}

// TestReplayRequest_NotFound 重放不存在的日志 ID 应返回 404
func TestReplayRequest_NotFound(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequest(t, router, "POST", "/admin/api/logs/99999/replay", nil)
	assert.Equal(t, http.StatusNotFound, rr.Code)

	result := decodeJSON(t, rr)
	assert.Contains(t, result, "error")
}

// === 模型自动发现 ===

// TestSetAutoDiscoverModels_Enable 启用上游模型自动发现
func TestSetAutoDiscoverModels_Enable(t *testing.T) {
	h, router := setupTestAdmin(t)

	up, err := h.store.CreateUpstream("discover-up", "https://api.openai.com", nil, 1, "", "round-robin", "api_key", "", false, false)
	require.NoError(t, err)

	urlPath := fmt.Sprintf("/admin/api/upstreams/%d/auto-discover", up.ID)
	rr := doRequest(t, router, "PUT", urlPath, map[string]interface{}{
		"enabled": true,
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, "updated", result["status"])
	assert.Equal(t, true, result["auto_discover_models"])
}

// TestSetAutoDiscoverModels_Disable 先启用再禁用模型自动发现
func TestSetAutoDiscoverModels_Disable(t *testing.T) {
	h, router := setupTestAdmin(t)

	up, err := h.store.CreateUpstream("discover-up-2", "https://api.openai.com", nil, 1, "", "round-robin", "api_key", "", false, false)
	require.NoError(t, err)

	urlPath := fmt.Sprintf("/admin/api/upstreams/%d/auto-discover", up.ID)

	// 先启用
	rr := doRequest(t, router, "PUT", urlPath, map[string]interface{}{
		"enabled": true,
	})
	require.Equal(t, http.StatusOK, rr.Code)

	// 再禁用
	rr = doRequest(t, router, "PUT", urlPath, map[string]interface{}{
		"enabled": false,
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, "updated", result["status"])
	assert.Equal(t, false, result["auto_discover_models"])
}

// === 上游拖拽排序 ===

// TestReorderUpstreams 创建 3 个上游后倒序排列，校验列表优先级已更新
func TestReorderUpstreams(t *testing.T) {
	h, router := setupTestAdmin(t)

	var ids []int64
	for _, name := range []string{"up-a", "up-b", "up-c"} {
		up, err := h.store.CreateUpstream(name, "https://api.openai.com", nil, 1, "", "round-robin", "api_key", "", false, false)
		require.NoError(t, err)
		ids = append(ids, up.ID)
	}

	// 倒序排列
	reversed := []int64{ids[2], ids[1], ids[0]}
	rr := doRequest(t, router, "PUT", "/admin/api/upstreams/reorder", map[string]interface{}{
		"ids": reversed,
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, "reordered", result["status"])

	// 获取上游列表校验优先级已按新顺序排列
	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	require.Len(t, list, 3)

	// 排序后 up-c 的优先级应最高（数字最小）
	// 找到各上游的 priority 值
	priorities := make(map[string]float64)
	for _, u := range list {
		priorities[u["name"].(string)] = u["priority"].(float64)
	}
	assert.Less(t, priorities["up-c"], priorities["up-a"], "up-c 应在 up-a 之前")
}

// TestReorderUpstreams_InvalidBody 发送非法 JSON 应返回 400
func TestReorderUpstreams_InvalidBody(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequestRawBody(t, router, "PUT", "/admin/api/upstreams/reorder", "not-json{{{")
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	result := decodeJSON(t, rr)
	assert.Contains(t, result, "error")
}

// === 快捷操作 ===

// TestPauseAllUpstreams 创建 2 个上游后一键暂停，校验列表中全部 disabled
func TestPauseAllUpstreams(t *testing.T) {
	h, router := setupTestAdmin(t)

	for _, name := range []string{"pause-a", "pause-b"} {
		_, err := h.store.CreateUpstream(name, "https://api.openai.com", nil, 1, "", "round-robin", "api_key", "", false, false)
		require.NoError(t, err)
	}

	rr := doRequest(t, router, "POST", "/admin/api/actions/pause-all", nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, "paused", result["status"])
	assert.Equal(t, float64(2), result["affected"])

	// 校验列表中全部 disabled
	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	for _, u := range list {
		assert.Equal(t, false, u["enabled"], "上游 %s 应已禁用", u["name"])
	}
}

// TestResumeAllUpstreams 先暂停再恢复，校验列表中全部 enabled
func TestResumeAllUpstreams(t *testing.T) {
	h, router := setupTestAdmin(t)

	for _, name := range []string{"resume-a", "resume-b"} {
		_, err := h.store.CreateUpstream(name, "https://api.openai.com", nil, 1, "", "round-robin", "api_key", "", false, false)
		require.NoError(t, err)
	}

	// 先暂停
	rr := doRequest(t, router, "POST", "/admin/api/actions/pause-all", nil)
	require.Equal(t, http.StatusOK, rr.Code)

	// 再恢复
	rr = doRequest(t, router, "POST", "/admin/api/actions/resume-all", nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, "resumed", result["status"])
	assert.Equal(t, float64(2), result["affected"])

	// 校验列表中全部 enabled
	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	for _, u := range list {
		assert.Equal(t, true, u["enabled"], "上游 %s 应已启用", u["name"])
	}
}

// TestRefreshAllCaches POST 刷新缓存，校验返回 200 + status=refreshed
func TestRefreshAllCaches(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequest(t, router, "POST", "/admin/api/actions/refresh-caches", nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, "refreshed", result["status"])
}

// === SSE 事件 ===

// TestSSEEvents_RequiresAuth 未携带认证 Token 时获取 SSE 事件应返回 401
func TestSSEEvents_RequiresAuth(t *testing.T) {
	_, router := setupTestAdmin(t)

	req := httptest.NewRequest("GET", "/admin/api/events", nil)
	// 不设置 Authorization 头
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

// === 认证中间件 ?token= 查询参数回退 ===

// TestAuthMiddleware_QueryParam 通过 ?token= 查询参数认证 SSE 端点应通过鉴权
func TestAuthMiddleware_QueryParam(t *testing.T) {
	_, router := setupTestAdmin(t)

	// SSE 端点会阻塞，用带超时的 context 确保不挂起
	urlPath := fmt.Sprintf("/admin/api/events?token=%s", testAdminToken)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	req := httptest.NewRequest("GET", urlPath, nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// 通过鉴权后 SSE 返回 200（text/event-stream），不是 401
	assert.NotEqual(t, http.StatusUnauthorized, rr.Code)
}

// TestAuthMiddleware_QueryParam_NonSSE 非 SSE 端点不接受 ?token= 认证
func TestAuthMiddleware_QueryParam_NonSSE(t *testing.T) {
	_, router := setupTestAdmin(t)

	urlPath := fmt.Sprintf("/admin/api/status?token=%s", testAdminToken)
	req := httptest.NewRequest("GET", urlPath, nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

// TestAuthMiddleware_QueryParam_Invalid 使用错误的 ?token= 应返回 401
func TestAuthMiddleware_QueryParam_Invalid(t *testing.T) {
	_, router := setupTestAdmin(t)

	req := httptest.NewRequest("GET", "/admin/api/status?token=wrong-token", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}
