package middleware

import (
	"log/slog"
	"sync/atomic"

	"github.com/Instawork/llm-proxy/internal/store"
)

// BindingCache 是 key-to-upstream 绑定关系的原子快照缓存。
// 用 atomic.Value 存储完整快照（map[int64][]int64），admin 修改后调用 Reload 刷新，
// 避免每个请求都查询 SQLite。
type BindingCache struct {
	data  atomic.Value // stores map[int64][]int64
	store *store.Store
}

// NewBindingCache 创建缓存并加载初始快照。
func NewBindingCache(s *store.Store) *BindingCache {
	c := &BindingCache{store: s}
	c.Reload()
	return c
}

// Reload 从数据库重新加载全部绑定关系到缓存。
// 加载失败时保持旧快照并打告警日志，避免服务中断。
func (c *BindingCache) Reload() {
	allBindings, err := c.store.GetAllKeyBindings()
	if err != nil {
		slog.Error("binding cache: failed to reload", "error", err)
		return
	}

	snapshot := make(map[int64][]int64, len(allBindings))
	for keyID, ids := range allBindings {
		cp := make([]int64, len(ids))
		copy(cp, ids)
		snapshot[keyID] = cp
	}
	c.data.Store(snapshot)
	slog.Info("binding cache: loaded", "keys_with_bindings", len(snapshot))
}

// GetKeyUpstreamIDs 返回某个 key 绑定的上游 ID 列表。返回 nil 表示无绑定。
func (c *BindingCache) GetKeyUpstreamIDs(keyID int64) []int64 {
	v := c.data.Load()
	if v == nil {
		return nil
	}
	return v.(map[int64][]int64)[keyID]
}
