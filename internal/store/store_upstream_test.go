package store

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Upstream CRUD
// ---------------------------------------------------------------------------

func TestUpstream_CreateAndGet(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("openai", "https://api.openai.com", []string{"sk-key123"}, 10, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	require.NotNil(t, up)
	assert.Positive(t, up.ID)
	assert.Equal(t, "openai", up.Name)
	assert.Equal(t, "https://api.openai.com", up.BaseURL)
	assert.Equal(t, []string{"sk-key123"}, up.APIKeys)
	assert.Equal(t, 10, up.Priority)
	assert.True(t, up.Healthy)

	got, err := s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.Equal(t, up.ID, got.ID)
	assert.Equal(t, "openai", got.Name)
	assert.Equal(t, []string{"sk-key123"}, got.APIKeys)
}

func TestUpstream_GetNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetUpstream(9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpstream_List(t *testing.T) {
	s := newTestStore(t)

	_, err := s.CreateUpstream("provider-a", "https://a.example.com", []string{"key-a"}, 5, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	_, err = s.CreateUpstream("provider-b", "https://b.example.com", []string{"key-b"}, 10, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	list, err := s.ListUpstreams()
	require.NoError(t, err)
	assert.Len(t, list, 2)

	// Verify decrypted API keys are returned
	names := make([]string, len(list))
	for i, u := range list {
		names[i] = u.Name
		assert.NotEmpty(t, u.APIKeys)
	}
	assert.Contains(t, names, "provider-a")
	assert.Contains(t, names, "provider-b")
}

func TestUpstream_Update(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("old-name", "https://old.example.com", []string{"old-key"}, 1, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	updated, err := s.UpdateUpstream(up.ID, "new-name", "https://new.example.com", []string{"new-key"}, 2, true, "", "", "", "", nil)
	require.NoError(t, err)
	assert.Equal(t, up.ID, updated.ID)
	assert.Equal(t, "new-name", updated.Name)
	assert.Equal(t, "https://new.example.com", updated.BaseURL)
	assert.Equal(t, []string{"new-key"}, updated.APIKeys)
	assert.Equal(t, 2, updated.Priority)
}

func TestUpstream_UpdateNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.UpdateUpstream(9999, "name", "https://example.com", []string{"key"}, 0, true, "", "", "", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpstream_Delete(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("to-delete", "https://example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.HardDeleteUpstream(up.ID)
	require.NoError(t, err)

	_, err = s.GetUpstream(up.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpstream_DeleteNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.HardDeleteUpstream(9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpstream_BatchSetEnabledAndDelete(t *testing.T) {
	s := newTestStore(t)
	u1, err := s.CreateUpstream("batch-a", "https://a.example.com", []string{"k1"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("batch-b", "https://b.example.com", []string{"k2"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u3, err := s.CreateUpstream("batch-c", "https://c.example.com", []string{"k3"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	// All created enabled by default.
	n, err := s.BatchSetUpstreamEnabled([]int64{u1.ID, u2.ID, u1.ID, 0, -1}, false)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	got1, err := s.GetUpstream(u1.ID)
	require.NoError(t, err)
	assert.False(t, got1.Enabled)
	got2, err := s.GetUpstream(u2.ID)
	require.NoError(t, err)
	assert.False(t, got2.Enabled)
	got3, err := s.GetUpstream(u3.ID)
	require.NoError(t, err)
	assert.True(t, got3.Enabled)

	n, err = s.BatchSetUpstreamEnabled([]int64{u1.ID, u2.ID}, true)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	deleted, err := s.BatchDeleteUpstreams([]int64{u1.ID, u3.ID, u1.ID})
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)

	_, err = s.GetUpstream(u1.ID)
	require.Error(t, err)
	_, err = s.GetUpstream(u3.ID)
	require.Error(t, err)
	_, err = s.GetUpstream(u2.ID)
	require.NoError(t, err)

	// Empty / unknown ids are no-ops, not errors.
	n, err = s.BatchSetUpstreamEnabled(nil, false)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
	deleted, err = s.BatchDeleteUpstreams([]int64{99999})
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)
}

func TestUpstream_APIKeyNotStoredAsPlaintext(t *testing.T) {
	s := newTestStore(t)
	plainKey := "sk-plaintext-secret-key"

	up, err := s.CreateUpstream("test", "https://example.com", []string{plainKey}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	// Read the raw api_key column from the upstream_api_keys table directly.
	var rawStored string
	err = s.db.QueryRow(`SELECT api_key FROM upstream_api_keys WHERE upstream_id = ?`, up.ID).Scan(&rawStored)
	require.NoError(t, err)

	assert.NotEqual(t, plainKey, rawStored, "plaintext key must not be stored in the database")
	assert.True(t, strings.HasPrefix(rawStored, "v1:"), "stored value should have encryption version prefix")
}

func TestUpstream_AddAndDeleteAPIKey(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://example.com", []string{"key-a"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	keys, err := s.AddUpstreamAPIKeys(up.ID, []string{"key-b", "key-c"})
	require.NoError(t, err)
	require.Len(t, keys, 3)
	assert.Equal(t, "key-a", keys[0].Key)
	assert.Equal(t, "key-b", keys[1].Key)
	assert.Equal(t, "key-c", keys[2].Key)

	err = s.DeleteUpstreamAPIKey(up.ID, keys[1].RowID)
	require.NoError(t, err)

	got, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"key-a", "key-c"}, []string{got[0].Key, got[1].Key})
}

func TestUpstream_DeleteAPIKeyNotFound(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.DeleteUpstreamAPIKey(up.ID, 9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpstream_AddAPIKeyNotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.AddUpstreamAPIKeys(9999, []string{"key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ===========================================================================
// EDGE-CASE / COVERAGE TESTS
// ===========================================================================

// ---------------------------------------------------------------------------
// CreateUpstream — edge cases
// ---------------------------------------------------------------------------

func TestCreateUpstream_EmptyAPIKeys_PublicUpstream(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("public", "https://public.example.com", []string{}, 5, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	assert.Empty(t, up.APIKeys, "public upstream should have no API keys")
	assert.Equal(t, 5, up.Priority)
	assert.True(t, up.Enabled)

	got, err := s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.Empty(t, got.APIKeys)
}

func TestCreateUpstream_NilAPIKeys_PublicUpstream(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("nil-keys", "https://nil.example.com", nil, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	assert.Empty(t, up.APIKeys)
}

func TestCreateUpstream_MultipleAPIKeys(t *testing.T) {
	s := newTestStore(t)

	keys := []string{"sk-key1", "sk-key2", "sk-key3"}
	up, err := s.CreateUpstream("multi-key", "https://multi.example.com", keys, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	assert.Equal(t, keys, up.APIKeys)

	got, err := s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.Equal(t, keys, got.APIKeys)
}

func TestCreateUpstream_WithRemark(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("remarked", "https://r.example.com", []string{"k"}, 0, "", "", "", "donated by Alice", false, false, 0)
	require.NoError(t, err)
	assert.Equal(t, "donated by Alice", up.Remark)

	got, err := s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.Equal(t, "donated by Alice", got.Remark)
}

func TestCreateUpstream_WithProxyURL(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("proxied", "https://p.example.com", []string{"k"}, 0, "http://proxy:8080", "", "", "", false, false, 0)
	require.NoError(t, err)
	assert.Equal(t, "http://proxy:8080", up.ProxyURL)

	got, err := s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.Equal(t, "http://proxy:8080", got.ProxyURL)
}

func TestCreateUpstream_AllAuthModes(t *testing.T) {
	s := newTestStore(t)

	// Default (empty string should become "api_key")
	up1, err := s.CreateUpstream("default-auth", "https://a.example.com", []string{"k"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	assert.Equal(t, "api_key", up1.AuthMode)

	// Explicit api_key
	up2, err := s.CreateUpstream("api-key-auth", "https://b.example.com", []string{"k"}, 0, "", "", "api_key", "", false, false, 0)
	require.NoError(t, err)
	assert.Equal(t, "api_key", up2.AuthMode)

	// OAuth
	up3, err := s.CreateUpstream("oauth-auth", "https://c.example.com", []string{"k"}, 0, "", "", "oauth", "", false, false, 0)
	require.NoError(t, err)
	assert.Equal(t, "oauth", up3.AuthMode)

	got, err := s.GetUpstream(up3.ID)
	require.NoError(t, err)
	assert.Equal(t, "oauth", got.AuthMode)
}

func TestCreateUpstream_DefaultKeySchedulingMode(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("sched-default", "https://s.example.com", []string{"k"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	assert.Equal(t, "round-robin", up.KeySchedulingMode)

	up2, err := s.CreateUpstream("sched-fill", "https://s2.example.com", []string{"k"}, 0, "", "fill", "", "", false, false, 0)
	require.NoError(t, err)
	assert.Equal(t, "fill", up2.KeySchedulingMode)
}

// ---------------------------------------------------------------------------
// UpdateUpstream — edge cases
// ---------------------------------------------------------------------------

func TestUpdateUpstream_UpdateBaseURL(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://old.example.com", []string{"k"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	updated, err := s.UpdateUpstream(up.ID, "up", "https://new.example.com", nil, 0, true, "", "", "", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "https://new.example.com", updated.BaseURL)
	// apiKeys=nil means keep existing keys
	assert.Equal(t, []string{"k"}, updated.APIKeys)
}

func TestUpdateUpstream_UpdateProxyURL(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	updated, err := s.UpdateUpstream(up.ID, "up", "https://u.example.com", nil, 0, true, "socks5://proxy:1080", "", "", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "socks5://proxy:1080", updated.ProxyURL)
}

func TestUpdateUpstream_ClearProxyURL(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k"}, 0, "http://proxy:8080", "", "", "", false, false, 0)
	require.NoError(t, err)

	updated, err := s.UpdateUpstream(up.ID, "up", "https://u.example.com", nil, 0, true, "", "", "", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "", updated.ProxyURL, "proxy_url should be cleared")
}

func TestUpdateUpstream_UpdateRemark(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k"}, 0, "", "", "", "old remark", false, false, 0)
	require.NoError(t, err)

	updated, err := s.UpdateUpstream(up.ID, "up", "https://u.example.com", nil, 0, true, "", "", "", "new remark", nil)
	require.NoError(t, err)
	assert.Equal(t, "new remark", updated.Remark)
}

func TestUpdateUpstream_UpdatePriority(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k"}, 1, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	updated, err := s.UpdateUpstream(up.ID, "up", "https://u.example.com", nil, 99, true, "", "", "", "", nil)
	require.NoError(t, err)
	assert.Equal(t, 99, updated.Priority)
}

func TestUpdateUpstream_UpdateModelPatterns_ViaUpdate(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	// Set patterns separately then update upstream metadata
	require.NoError(t, s.SetUpstreamModelPatterns(up.ID, []string{"gpt-*"}))

	updated, err := s.UpdateUpstream(up.ID, "renamed", "https://u.example.com", nil, 5, true, "", "", "", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "renamed", updated.Name)

	// Patterns should still exist after upstream metadata update
	patterns, err := s.GetUpstreamModelPatterns(up.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-*"}, patterns)
}

func TestUpdateUpstream_NilAPIKeysKeepsExisting(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"key-a", "key-b"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	updated, err := s.UpdateUpstream(up.ID, "up", "https://u.example.com", nil, 0, true, "", "", "", "", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"key-a", "key-b"}, updated.APIKeys, "nil apiKeys should preserve existing keys")
}

// ---------------------------------------------------------------------------
// SetWebSocketEnabled（WebSocket 透传开关）
// ---------------------------------------------------------------------------

func TestSetWebSocketEnabled(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("ws-up", "https://ws.example.com", []string{"k"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	assert.False(t, up.WebSocketEnabled, "默认应为 false")

	// 启用 WebSocket
	err = s.SetWebSocketEnabled(up.ID, true)
	require.NoError(t, err)

	got, err := s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.True(t, got.WebSocketEnabled, "应已启用 WebSocket")

	// 禁用 WebSocket
	err = s.SetWebSocketEnabled(up.ID, false)
	require.NoError(t, err)

	got, err = s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.False(t, got.WebSocketEnabled, "应已禁用 WebSocket")
}

func TestSetWebSocketEnabled_NotFound(t *testing.T) {
	s := newTestStore(t)

	err := s.SetWebSocketEnabled(99999, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------------------------------------------------------------------------
// SetAutoDiscoverModels
// ---------------------------------------------------------------------------

func TestSetAutoDiscoverModels(t *testing.T) {
	s := newTestStore(t)

	// 创建上游，autoDiscoverModels 默认为 false
	up, err := s.CreateUpstream("discover-up", "https://d.example.com", []string{"k"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	assert.False(t, up.AutoDiscoverModels, "默认应为 false")

	// 启用模型自动发现
	err = s.SetAutoDiscoverModels(up.ID, true)
	require.NoError(t, err)

	got, err := s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.True(t, got.AutoDiscoverModels, "应已启用模型自动发现")

	// 再次禁用
	err = s.SetAutoDiscoverModels(up.ID, false)
	require.NoError(t, err)

	got, err = s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.False(t, got.AutoDiscoverModels, "应已禁用模型自动发现")
}

func TestSetAutoDiscoverModels_NotFound(t *testing.T) {
	s := newTestStore(t)

	err := s.SetAutoDiscoverModels(99999, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------------------------------------------------------------------------
// UpdateDiscoveredModels
// ---------------------------------------------------------------------------

func TestUpdateDiscoveredModels(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("disc-up", "https://disc.example.com", []string{"k"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	// 初始状态：无模型模式，无发现时间
	patterns, err := s.GetUpstreamModelPatterns(up.ID)
	require.NoError(t, err)
	assert.Empty(t, patterns)

	got, err := s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.Nil(t, got.LastModelDiscovery, "初始 last_model_discovery 应为空")

	// 更新发现的模型
	err = s.UpdateDiscoveredModels(up.ID, []string{"gpt-4o", "claude-*"})
	require.NoError(t, err)

	// 验证模式已持久化
	patterns, err = s.GetUpstreamModelPatterns(up.ID)
	require.NoError(t, err)
	assert.Len(t, patterns, 2)
	assert.Contains(t, patterns, "gpt-4o")
	assert.Contains(t, patterns, "claude-*")

	// 验证 last_model_discovery 已被设置
	got, err = s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.LastModelDiscovery, "last_model_discovery 应已被设置")
	assert.WithinDuration(t, time.Now().UTC(), *got.LastModelDiscovery, 5*time.Second)
}

// ---------------------------------------------------------------------------
// ReorderUpstreams
// ---------------------------------------------------------------------------

func TestReorderUpstreams(t *testing.T) {
	s := newTestStore(t)

	u1, err := s.CreateUpstream("reorder-a", "https://a.example.com", []string{"ka"}, 10, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("reorder-b", "https://b.example.com", []string{"kb"}, 20, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u3, err := s.CreateUpstream("reorder-c", "https://c.example.com", []string{"kc"}, 30, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	// 重排为 [u3, u1, u2]，优先级应变为 0, 1, 2
	err = s.ReorderUpstreams([]int64{u3.ID, u1.ID, u2.ID})
	require.NoError(t, err)

	got3, err := s.GetUpstream(u3.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, got3.Priority, "u3 应排在位置 0")

	got1, err := s.GetUpstream(u1.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, got1.Priority, "u1 应排在位置 1")

	got2, err := s.GetUpstream(u2.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, got2.Priority, "u2 应排在位置 2")
}

func TestReorderUpstreams_EmptyList(t *testing.T) {
	s := newTestStore(t)

	// 创建一些上游确保操作不会影响它们
	u1, err := s.CreateUpstream("empty-reorder", "https://a.example.com", []string{"ka"}, 5, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	// 空列表应为 no-op
	err = s.ReorderUpstreams([]int64{})
	require.NoError(t, err)

	// 验证原始优先级未变
	got, err := s.GetUpstream(u1.ID)
	require.NoError(t, err)
	assert.Equal(t, 5, got.Priority, "空列表不应改变任何优先级")
}

// ---------------------------------------------------------------------------
// SetAllUpstreamsEnabled
// ---------------------------------------------------------------------------

func TestSetAllUpstreamsEnabled_Disable(t *testing.T) {
	s := newTestStore(t)

	_, err := s.CreateUpstream("all-en-a", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	_, err = s.CreateUpstream("all-en-b", "https://b.example.com", []string{"kb"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	// 禁用所有
	n, err := s.SetAllUpstreamsEnabled(false)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	// 验证全部已禁用
	upstreams, err := s.ListUpstreams()
	require.NoError(t, err)
	for _, u := range upstreams {
		assert.False(t, u.Enabled, "所有上游应已禁用")
	}
}

func TestSetAllUpstreamsEnabled_Enable(t *testing.T) {
	s := newTestStore(t)

	u1, err := s.CreateUpstream("all-re-a", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("all-re-b", "https://b.example.com", []string{"kb"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	// 先禁用所有
	_, err = s.SetAllUpstreamsEnabled(false)
	require.NoError(t, err)

	// 再启用所有
	n, err := s.SetAllUpstreamsEnabled(true)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	got1, err := s.GetUpstream(u1.ID)
	require.NoError(t, err)
	assert.True(t, got1.Enabled, "u1 应已启用")

	got2, err := s.GetUpstream(u2.ID)
	require.NoError(t, err)
	assert.True(t, got2.Enabled, "u2 应已启用")
}

func TestSetAllUpstreamsEnabled_ReturnsCount(t *testing.T) {
	s := newTestStore(t)

	_, err := s.CreateUpstream("cnt-a", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	_, err = s.CreateUpstream("cnt-b", "https://b.example.com", []string{"kb"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	_, err = s.CreateUpstream("cnt-c", "https://c.example.com", []string{"kc"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	// 禁用全部 3 个
	n, err := s.SetAllUpstreamsEnabled(false)
	require.NoError(t, err)
	assert.Equal(t, int64(3), n, "应返回受影响行数 3")

	// 再次禁用（已经禁用），仍应返回 3（UPDATE 影响的行数包括值未变的行）
	n, err = s.SetAllUpstreamsEnabled(false)
	require.NoError(t, err)
	assert.Equal(t, int64(3), n, "即使值未变也应返回匹配行数")

	// 空数据库的情况
	s2 := newTestStore(t)
	n, err = s2.SetAllUpstreamsEnabled(true)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n, "空数据库应返回 0")
}

// ---------------------------------------------------------------------------
// 软删除 / 撤销删除 / 清理 / 硬删除
// ---------------------------------------------------------------------------

// TestSoftDeleteUpstream 软删除后应从 ListUpstreams 消失，但出现在 ListDeletedUpstreams 中
func TestSoftDeleteUpstream(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("soft-del", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SoftDeleteUpstream(up.ID)
	require.NoError(t, err)

	// 不应出现在正常列表中
	list, err := s.ListUpstreams()
	require.NoError(t, err)
	for _, u := range list {
		assert.NotEqual(t, up.ID, u.ID, "软删除的上游不应出现在 ListUpstreams 中")
	}

	// 应出现在已删除列表中
	deleted, err := s.ListDeletedUpstreams()
	require.NoError(t, err)
	require.Len(t, deleted, 1)
	assert.Equal(t, up.ID, deleted[0].ID)
	assert.NotNil(t, deleted[0].DeletedAt, "DeletedAt 应非空")
}

// TestUndoDeleteUpstream 软删除后撤销，应重新出现在 ListUpstreams 中
func TestUndoDeleteUpstream(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("undo-del", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SoftDeleteUpstream(up.ID)
	require.NoError(t, err)

	err = s.UndoDeleteUpstream(up.ID)
	require.NoError(t, err)

	// 应重新出现在正常列表中
	list, err := s.ListUpstreams()
	require.NoError(t, err)
	found := false
	for _, u := range list {
		if u.ID == up.ID {
			found = true
			assert.Nil(t, u.DeletedAt, "撤销后 DeletedAt 应为 nil")
		}
	}
	assert.True(t, found, "撤销删除后应重新出现在 ListUpstreams 中")

	// 不应出现在已删除列表中
	deleted, err := s.ListDeletedUpstreams()
	require.NoError(t, err)
	assert.Empty(t, deleted)
}

// TestPurgeDeletedUpstreams 软删除后立即清理（olderThan=0），应彻底消失
func TestPurgeDeletedUpstreams(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("purge-del", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SoftDeleteUpstream(up.ID)
	require.NoError(t, err)

	// olderThan=0 意味着所有已删除的都应被清理
	purged, err := s.PurgeDeletedUpstreams(0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), purged)

	// GetUpstream 也应找不到
	_, err = s.GetUpstream(up.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// 已删除列表也应为空
	deleted, err := s.ListDeletedUpstreams()
	require.NoError(t, err)
	assert.Empty(t, deleted)
}

// TestHardDeleteUpstream 硬删除应级联清除关联的 API Key 和绑定
func TestHardDeleteUpstream(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("hard-del", "https://a.example.com", []string{"key-a", "key-b"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	// 先设置一些模型模式，验证级联
	err = s.SetUpstreamModelPatterns(up.ID, []string{"gpt-*"})
	require.NoError(t, err)

	err = s.HardDeleteUpstream(up.ID)
	require.NoError(t, err)

	// 上游应完全消失
	_, err = s.GetUpstream(up.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// API Key 应级联清除
	keys, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)
	assert.Empty(t, keys)

	// 模型模式应级联清除
	patterns, err := s.GetUpstreamModelPatterns(up.ID)
	require.NoError(t, err)
	assert.Empty(t, patterns)
}

// ---------------------------------------------------------------------------
// 上游排序（使用新参数创建）
// ---------------------------------------------------------------------------

// TestReorderUpstreams_WithNewParams 创建 3 个上游后重新排序，验证优先级更新
func TestReorderUpstreams_WithNewParams(t *testing.T) {
	s := newTestStore(t)

	u1, err := s.CreateUpstream("reorder-a", "https://a.example.com", []string{"ka"}, 10, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("reorder-b", "https://b.example.com", []string{"kb"}, 20, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u3, err := s.CreateUpstream("reorder-c", "https://c.example.com", []string{"kc"}, 30, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	// 倒序排列：C(0), B(1), A(2)
	err = s.ReorderUpstreams([]int64{u3.ID, u2.ID, u1.ID})
	require.NoError(t, err)

	list, err := s.ListUpstreams()
	require.NoError(t, err)
	require.Len(t, list, 3)

	// ListUpstreams 按 priority ASC 排序，所以第一个应是 u3
	assert.Equal(t, u3.ID, list[0].ID, "u3 应排在最前面（优先级 0）")
	assert.Equal(t, 0, list[0].Priority)
	assert.Equal(t, u2.ID, list[1].ID, "u2 应排第二（优先级 1）")
	assert.Equal(t, 1, list[1].Priority)
	assert.Equal(t, u1.ID, list[2].ID, "u1 应排最后（优先级 2）")
	assert.Equal(t, 2, list[2].Priority)
}

// ---------------------------------------------------------------------------
// 上游模板
// ---------------------------------------------------------------------------

// TestGetUpstreamTemplates 验证返回非空的预置模板列表，包含已知提供商
func TestGetUpstreamTemplates(t *testing.T) {
	templates := GetUpstreamTemplates()
	assert.NotEmpty(t, templates, "模板列表不应为空")

	// 验证包含已知的提供商
	names := make([]string, len(templates))
	for i, tmpl := range templates {
		names[i] = tmpl.Name
		assert.NotEmpty(t, tmpl.BaseURL, "模板 %s 的 BaseURL 不应为空", tmpl.Name)
		assert.NotEmpty(t, tmpl.AuthMode, "模板 %s 的 AuthMode 不应为空", tmpl.Name)
		assert.NotEmpty(t, tmpl.ModelPatterns, "模板 %s 的 ModelPatterns 不应为空", tmpl.Name)
	}
	assert.Contains(t, names, "OpenAI")
	assert.Contains(t, names, "Anthropic")
}
