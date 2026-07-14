package admin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// doRequestRawBody 发送原始字符串 body 的请求（用于测试非法 JSON 和超大 body）
func doRequestRawBody(t *testing.T, router *mux.Router, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	authRequest(req)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

// === 延迟统计 ===

// TestGetLatencyStats_Empty 无日志时返回空数组
func TestGetLatencyStats_Empty(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequest(t, router, "GET", "/admin/api/stats/latency", nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSONArray(t, rr)
	assert.Empty(t, result)
}

// TestGetLatencyStats_WithData 插入请求日志后查询延迟统计，校验返回结构
func TestGetLatencyStats_WithData(t *testing.T) {
	h, router := setupTestAdmin(t)

	// 先创建一个上游，然后插入请求日志
	_, err := h.store.CreateUpstream("test-up", "https://api.openai.com", nil, 1, "", "round-robin", "api_key", "", false, false)
	require.NoError(t, err)

	logs := []store.RequestLog{
		{
			DownstreamKeyID: 0,
			UpstreamName:    "test-up",
			UpstreamKeyIdx:  0,
			Model:           "gpt-4",
			StatusCode:      200,
			LatencyMs:       150,
			CreatedAt:       time.Now().UTC(),
		},
		{
			DownstreamKeyID: 0,
			UpstreamName:    "test-up",
			UpstreamKeyIdx:  0,
			Model:           "gpt-4",
			StatusCode:      200,
			LatencyMs:       250,
			CreatedAt:       time.Now().UTC(),
		},
	}
	err = h.store.InsertRequestLogBatch(logs)
	require.NoError(t, err)

	rr := doRequest(t, router, "GET", "/admin/api/stats/latency?hours=24", nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSONArray(t, rr)
	require.NotEmpty(t, result)
	// 校验返回结构包含预期字段
	first := result[0]
	assert.Contains(t, first, "upstream_name")
	assert.Contains(t, first, "total")
	assert.Contains(t, first, "avg_latency")
	assert.Contains(t, first, "min_latency")
	assert.Contains(t, first, "max_latency")
	assert.Equal(t, "test-up", first["upstream_name"])
	assert.Equal(t, float64(2), first["total"])
}

// TestGetLatencyStats_HoursCapped hours=9999 应被截断到 720 而非报错
func TestGetLatencyStats_HoursCapped(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequest(t, router, "GET", "/admin/api/stats/latency?hours=9999", nil)
	// 不应返回错误——handler 将 hours 截断到 720
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSONArray(t, rr)
	assert.NotNil(t, result) // 空数组也可以，不能是 null
}

// === 健康历史 ===

// TestGetHealthHistory_Empty 无历史时返回空数组
func TestGetHealthHistory_Empty(t *testing.T) {
	h, router := setupTestAdmin(t)

	// 需要先创建上游
	up, err := h.store.CreateUpstream("health-up", "https://api.openai.com", nil, 1, "", "round-robin", "api_key", "", false, false)
	require.NoError(t, err)

	urlPath := fmt.Sprintf("/admin/api/upstreams/%d/health-history", up.ID)
	rr := doRequest(t, router, "GET", urlPath, nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSONArray(t, rr)
	assert.Empty(t, result)
}

// TestGetHealthHistory_WithData 插入探针记录后查询，校验返回数据
func TestGetHealthHistory_WithData(t *testing.T) {
	h, router := setupTestAdmin(t)

	up, err := h.store.CreateUpstream("health-up-2", "https://api.openai.com", nil, 1, "", "round-robin", "api_key", "", false, false)
	require.NoError(t, err)

	// 插入探针记录
	err = h.store.RecordHealthProbe(up.ID, true, 120, "")
	require.NoError(t, err)
	err = h.store.RecordHealthProbe(up.ID, false, 0, "connection refused")
	require.NoError(t, err)

	urlPath := fmt.Sprintf("/admin/api/upstreams/%d/health-history?hours=24&limit=10", up.ID)
	rr := doRequest(t, router, "GET", urlPath, nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSONArray(t, rr)
	require.Len(t, result, 2)
	// 按时间倒序排列，最后插入的在前
	assert.Equal(t, false, result[0]["healthy"])
	assert.Equal(t, true, result[1]["healthy"])
}

// === 配置导出 ===

// TestExportConfig 创建上游和 Key 后导出，校验 JSON 结构且不泄露 API Key
func TestExportConfig(t *testing.T) {
	h, router := setupTestAdmin(t)

	// 创建上游（含 API Key）
	_, err := h.store.CreateUpstream("export-up", "https://api.openai.com", []string{"sk-secret-key"}, 1, "", "round-robin", "api_key", "test remark", false, false)
	require.NoError(t, err)

	// 创建下游 Key
	_, _, err = h.store.CreateKey("export-key", 100)
	require.NoError(t, err)

	rr := doRequest(t, router, "GET", "/admin/api/config/export", nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	// 校验顶层结构
	assert.Contains(t, result, "version")
	assert.Contains(t, result, "exported_at")
	assert.Contains(t, result, "upstreams")
	assert.Contains(t, result, "keys")
	assert.Contains(t, result, "whitelist")
	assert.Contains(t, result, "settings")

	// 校验上游信息
	upstreams := result["upstreams"].([]interface{})
	require.Len(t, upstreams, 1)
	upObj := upstreams[0].(map[string]interface{})
	assert.Equal(t, "export-up", upObj["name"])
	assert.Equal(t, "https://api.openai.com", upObj["base_url"])

	// 确认不泄露上游 API Key 明文
	body := rr.Body.String()
	assert.NotContains(t, body, "sk-secret-key", "导出配置不应包含上游 API Key 明文")

	// 校验下游 Key 信息
	keys := result["keys"].([]interface{})
	require.Len(t, keys, 1)
	keyObj := keys[0].(map[string]interface{})
	assert.Equal(t, "export-key", keyObj["name"])
}

// === 配置导入 ===

// TestImportConfig POST 合法配置 JSON，校验上游和 Key 被创建
func TestImportConfig(t *testing.T) {
	_, router := setupTestAdmin(t)

	cfg := store.ConfigExport{
		Version:    "1",
		ExportedAt: time.Now().UTC(),
		Upstreams: []store.UpstreamExport{
			{
				Name:              "imported-up",
				BaseURL:           "https://api.openai.com",
				Priority:          5,
				Enabled:           true,
				KeySchedulingMode: "round-robin",
				AuthMode:          "api_key",
			},
		},
		Keys: []store.KeyExport{
			{
				Name:     "imported-key",
				RPMLimit: 60,
				Enabled:  true,
			},
		},
		Whitelist: []string{"gpt-*"},
		Settings:  map[string]string{"auto_disable_threshold": "10"},
	}

	rr := doRequest(t, router, "POST", "/admin/api/config/import", cfg)
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)
	assert.Equal(t, "imported", result["status"])

	// 校验上游已创建
	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	upstreams := decodeJSONArray(t, rr)
	require.Len(t, upstreams, 1)
	assert.Equal(t, "imported-up", upstreams[0]["name"])

	// 校验 Key 已创建
	rr = doRequest(t, router, "GET", "/admin/api/keys", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	keys := decodeJSONArray(t, rr)
	require.Len(t, keys, 1)
	assert.Equal(t, "imported-key", keys[0]["name"])
}

// TestImportConfig_InvalidJSON POST 非法 JSON 应返回 400
func TestImportConfig_InvalidJSON(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequestRawBody(t, router, "POST", "/admin/api/config/import", "this is not json{{{")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// TestImportConfig_TooLarge POST >10MB 应返回 400（MaxBytesReader 限制）
func TestImportConfig_TooLarge(t *testing.T) {
	_, router := setupTestAdmin(t)

	// 构造 >10MB 的 body
	largeBody := `{"version":"1","upstreams":[],"keys":[],"whitelist":[],"settings":{"x":"` + strings.Repeat("A", 11*1024*1024) + `"}}`
	rr := doRequestRawBody(t, router, "POST", "/admin/api/config/import", largeBody)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// === Settings（slow_request_threshold_ms + log_retention_days）===

// TestGetSettings_IncludesNewFields GET /admin/api/settings 应包含 slow_request_threshold_ms 和 log_retention_days
func TestGetSettings_IncludesNewFields(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequest(t, router, "GET", "/admin/api/settings", nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Contains(t, result, "slow_request_threshold_ms")
	assert.Contains(t, result, "log_retention_days")
	assert.Contains(t, result, "auto_disable_threshold")

	// 默认值校验
	assert.Equal(t, float64(30000), result["slow_request_threshold_ms"])
	assert.Equal(t, float64(15), result["log_retention_days"])
}

// TestUpdateSettings_SlowThreshold PUT 设置 slow_request_threshold_ms 后校验持久化
func TestUpdateSettings_SlowThreshold(t *testing.T) {
	_, router := setupTestAdmin(t)

	// 更新 slow_request_threshold_ms
	rr := doRequest(t, router, "PUT", "/admin/api/settings", map[string]interface{}{
		"slow_request_threshold_ms": 5000,
	})
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)
	assert.Equal(t, "updated", result["status"])

	// 再次 GET 验证持久化
	rr = doRequest(t, router, "GET", "/admin/api/settings", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	result = decodeJSON(t, rr)
	assert.Equal(t, float64(5000), result["slow_request_threshold_ms"])
}

// === Key CRUD with max_concurrent ===

// TestCreateKey_WithMaxConcurrent 创建 Key 时指定 max_concurrent，校验列表返回
func TestCreateKey_WithMaxConcurrent(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequest(t, router, "POST", "/admin/api/keys", map[string]interface{}{
		"name":           "concurrent-key",
		"rpm_limit":      100,
		"max_concurrent": 5,
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	assert.Equal(t, float64(5), created["max_concurrent"])

	// 列表中也应包含 max_concurrent
	rr = doRequest(t, router, "GET", "/admin/api/keys", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	require.Len(t, list, 1)
	assert.Equal(t, float64(5), list[0]["max_concurrent"])
}

// TestUpdateKey_MaxConcurrent 更新 Key 的 max_concurrent，校验返回值
func TestUpdateKey_MaxConcurrent(t *testing.T) {
	_, router := setupTestAdmin(t)

	// 先创建 Key
	rr := doRequest(t, router, "POST", "/admin/api/keys", map[string]interface{}{
		"name":           "update-mc-key",
		"rpm_limit":      50,
		"max_concurrent": 3,
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	id := created["id"]

	// 更新 max_concurrent
	urlPath := fmt.Sprintf("/admin/api/keys/%v", id)
	rr = doRequest(t, router, "PUT", urlPath, map[string]interface{}{
		"max_concurrent": 10,
	})
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)
	assert.Equal(t, float64(10), result["max_concurrent"])

	// 列表中校验更新后的值
	rr = doRequest(t, router, "GET", "/admin/api/keys", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	require.Len(t, list, 1)
	assert.Equal(t, float64(10), list[0]["max_concurrent"])
}
