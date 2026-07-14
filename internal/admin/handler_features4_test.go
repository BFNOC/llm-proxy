package admin

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 上游模板：GET /admin/api/upstream-templates
// ---------------------------------------------------------------------------

// TestListUpstreamTemplates 获取模板列表，验证返回非空数组
func TestListUpstreamTemplates(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	rec := featDoReq(t, router, "GET", "/admin/api/upstream-templates", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	templates := featDecodeArray(t, rec)
	assert.NotEmpty(t, templates, "模板列表不应为空")

	// 验证至少包含 OpenAI
	found := false
	for _, tmpl := range templates {
		if tmpl["name"] == "OpenAI" {
			found = true
			assert.NotEmpty(t, tmpl["base_url"])
			assert.NotEmpty(t, tmpl["auth_mode"])
			break
		}
	}
	assert.True(t, found, "模板列表应包含 OpenAI")
}

// ---------------------------------------------------------------------------
// 上游 RPM 限制：PUT /admin/api/upstreams/{id}/rpm-limit
// ---------------------------------------------------------------------------

// TestSetUpstreamRPMLimit_Handler 设置上游 RPM 限制，验证响应
func TestSetUpstreamRPMLimit_Handler(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	uID := seedUpstream(t, s, "rpm-limit-up")

	rec := featDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/rpm-limit", uID), map[string]interface{}{
		"rpm_limit": 500,
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := featDecodeMap(t, rec)
	assert.Equal(t, "updated", resp["status"])
	assert.Equal(t, float64(500), resp["rpm_limit"])

	// 通过 store 验证持久化
	got, err := s.GetUpstream(uID)
	require.NoError(t, err)
	assert.Equal(t, 500, got.UpstreamRPMLimit)
}

// ---------------------------------------------------------------------------
// 熔断器配置：PUT /admin/api/upstreams/{id}/circuit-breaker
// ---------------------------------------------------------------------------

// TestSetCircuitBreakerConfig_Handler 设置熔断器配置，验证响应
func TestSetCircuitBreakerConfig_Handler(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	uID := seedUpstream(t, s, "cb-config-up")

	rec := featDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/circuit-breaker", uID), map[string]interface{}{
		"threshold":        5,
		"recovery_seconds": 30,
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := featDecodeMap(t, rec)
	assert.Equal(t, "updated", resp["status"])

	// 通过 store 验证持久化
	got, err := s.GetUpstream(uID)
	require.NoError(t, err)
	assert.Equal(t, 5, got.CircuitBreakerThreshold)
	assert.Equal(t, 30, got.CircuitBreakerRecoverySeconds)
}

// ---------------------------------------------------------------------------
// 软删除与撤销：DELETE /admin/api/upstreams/{id} + POST /admin/api/upstreams/{id}/undo
// ---------------------------------------------------------------------------

// TestSoftDeleteAndUndo 软删除上游后验证 undo_seconds 字段，撤销后验证上游恢复
func TestSoftDeleteAndUndo(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	uID := seedUpstream(t, s, "soft-del-undo")

	// 软删除
	rec := featDoReq(t, router, "DELETE", fmt.Sprintf("/admin/api/upstreams/%d", uID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	delResp := featDecodeMap(t, rec)
	assert.Equal(t, "deleted", delResp["status"])
	assert.Contains(t, delResp, "undo_seconds", "软删除响应应包含 undo_seconds")

	// 验证不在正常列表中
	rec = featDoReq(t, router, "GET", "/admin/api/upstreams", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	list := featDecodeArray(t, rec)
	for _, u := range list {
		assert.NotEqual(t, float64(uID), u["id"], "软删除的上游不应出现在列表中")
	}

	// 撤销删除
	rec = featDoReq(t, router, "POST", fmt.Sprintf("/admin/api/upstreams/%d/undo", uID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	undoResp := featDecodeMap(t, rec)
	assert.Equal(t, "restored", undoResp["status"])

	// 验证重新出现在列表中
	rec = featDoReq(t, router, "GET", "/admin/api/upstreams", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	list = featDecodeArray(t, rec)
	found := false
	for _, u := range list {
		if u["id"] == float64(uID) {
			found = true
		}
	}
	assert.True(t, found, "撤销后上游应重新出现在列表中")
}

// ---------------------------------------------------------------------------
// 已删除上游列表：GET /admin/api/upstreams/deleted
// ---------------------------------------------------------------------------

// TestListDeletedUpstreams 软删除后应出现在已删除列表中
func TestListDeletedUpstreams(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	uID := seedUpstream(t, s, "deleted-list")

	// 软删除
	rec := featDoReq(t, router, "DELETE", fmt.Sprintf("/admin/api/upstreams/%d", uID), nil)
	require.Equal(t, http.StatusOK, rec.Code)

	// 获取已删除列表
	rec = featDoReq(t, router, "GET", "/admin/api/upstreams/deleted", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	deleted := featDecodeArray(t, rec)
	require.NotEmpty(t, deleted, "已删除列表不应为空")

	found := false
	for _, u := range deleted {
		if u["ID"] == float64(uID) || u["id"] == float64(uID) {
			found = true
		}
	}
	assert.True(t, found, "软删除的上游应出现在已删除列表中")
}

// ---------------------------------------------------------------------------
// 熔断状态：GET /admin/api/upstreams/circuit-status
// ---------------------------------------------------------------------------

// TestGetCircuitStatus 获取熔断状态应返回 200
func TestGetCircuitStatus(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	rec := featDoReq(t, router, "GET", "/admin/api/upstreams/circuit-status", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// circuitBreaker 在 setupTestAdminWithStore 中为 nil，应返回空 map
	resp := featDecodeMap(t, rec)
	assert.NotNil(t, resp)
}
