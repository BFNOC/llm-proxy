package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync/atomic"

	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
)

// ModelOverrideCache 是 per-key 模型路由覆盖的原子快照缓存。
// 用 atomic.Value 存储完整快照，admin 修改后调用 Reload 刷新，
// 避免每个请求都查询 SQLite。
type ModelOverrideCache struct {
	data  atomic.Value // stores map[int64][]proxy.KeyModelOverrideRule
	store *store.Store
}

// NewModelOverrideCache 创建缓存并加载初始快照。
func NewModelOverrideCache(s *store.Store) *ModelOverrideCache {
	c := &ModelOverrideCache{store: s}
	c.Reload()
	return c
}

// Reload 从数据库重新加载全部覆盖规则到缓存。
// 加载失败时保持旧快照并打告警日志，避免服务中断。
func (c *ModelOverrideCache) Reload() {
	allOverrides, err := c.store.GetAllKeyModelOverrides()
	if err != nil {
		slog.Error("model override cache: failed to reload", "error", err)
		return
	}

	snapshot := make(map[int64][]proxy.KeyModelOverrideRule)
	for keyID, overrides := range allOverrides {
		snapshot[keyID] = convertToRules(overrides)
	}
	c.data.Store(snapshot)
	slog.Info("model override cache: loaded", "keys_with_overrides", len(snapshot))
}

// Get 返回某个 key 的覆盖规则。返回 nil 表示无覆盖。
func (c *ModelOverrideCache) Get(keyID int64) []proxy.KeyModelOverrideRule {
	v := c.data.Load()
	if v == nil {
		return nil
	}
	return v.(map[int64][]proxy.KeyModelOverrideRule)[keyID]
}

// convertToRules 把 store 层的平铺覆盖记录转换为运行时规则格式。
// 相同 pattern 的多行合并为一个 rule 的多个 upstream ID。
// 加载时同时校验 pattern 语法，跳过非法 pattern 并告警。
func convertToRules(overrides []store.KeyModelOverride) []proxy.KeyModelOverrideRule {
	// 按 pattern 分组
	grouped := make(map[string][]int64)
	var order []string // 保持稳定顺序
	for _, o := range overrides {
		// 校验 pattern 语法
		if strings.Contains(o.ModelPattern, "*") || strings.Contains(o.ModelPattern, "?") {
			if _, err := path.Match(o.ModelPattern, "test"); err != nil {
				slog.Warn("model override cache: invalid pattern, skipping",
					"key_id", o.DownstreamKeyID, "pattern", o.ModelPattern, "error", err)
				continue
			}
		}
		if _, exists := grouped[o.ModelPattern]; !exists {
			order = append(order, o.ModelPattern)
		}
		grouped[o.ModelPattern] = append(grouped[o.ModelPattern], o.UpstreamID)
	}

	rules := make([]proxy.KeyModelOverrideRule, 0, len(order))
	for _, p := range order {
		rules = append(rules, proxy.KeyModelOverrideRule{
			ModelPattern: p,
			UpstreamIDs:  grouped[p],
		})
	}
	return rules
}

// UpstreamBindingMiddleware 根据已解析出的下游 Key 查询其显式绑定的上游 ID，
// 并把结果写入请求上下文，供 DynamicProxy 在真正转发前做授权过滤。
// 约定"没有绑定记录"表示不限制上游，以兼容未配置绑定的历史 Key。
// 一旦绑定查询失败，直接拒绝请求（fail-closed），
// 避免存储异常时请求意外绕过上游访问控制。
//
// 同时从 overrideCache 读取 per-key 模型路由覆盖写入 context。
func UpstreamBindingMiddleware(s *store.Store, overrideCache *ModelOverrideCache) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			keyID := DownstreamKeyIDFromContext(r.Context())
			// 没有解析出 keyID 时不写入任何限制，保留默认的"允许全部上游"语义。
			if keyID == 0 {
				next.ServeHTTP(w, r)
				return
			}

			ids, err := s.GetKeyUpstreamIDs(keyID)
			if err != nil {
				slog.Error("binding: failed to get key upstream bindings", "key_id", keyID, "error", err)
				// 绑定读取失败时直接拒绝，而不是回退到"允许全部"，避免授权边界被静默放宽。
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				json.NewEncoder(w).Encode(map[string]string{"error": "upstream binding lookup failed"}) //nolint:errcheck
				return
			}

			ctx := r.Context()

			// 只有存在显式绑定时才写入 context；nil/空切片被保留为"未绑定"的哨兵语义。
			if len(ids) > 0 {
				ctx = proxy.ContextWithAllowedUpstreamIDs(ctx, ids)
			}

			// 从缓存读取 per-key 模型路由覆盖
			if overrideCache != nil {
				if rules := overrideCache.Get(keyID); len(rules) > 0 {
					ctx = proxy.ContextWithKeyModelOverrides(ctx, rules)
				}
			}

			r = r.WithContext(ctx)
			next.ServeHTTP(w, r)
		})
	}
}

