package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
)

// UpstreamBindingMiddleware 根据已解析出的下游 Key 查询其显式绑定的上游 ID，
// 并把结果写入请求上下文，供 DynamicProxy 在真正转发前做授权过滤。
// 约定"没有绑定记录"表示不限制上游，以兼容未配置绑定的历史 Key。
// 一旦绑定查询失败，直接拒绝请求（fail-closed），
// 避免存储异常时请求意外绕过上游访问控制。
func UpstreamBindingMiddleware(s *store.Store) func(http.Handler) http.Handler {
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

			// 只有存在显式绑定时才写入 context；nil/空切片被保留为"未绑定"的哨兵语义。
			if len(ids) > 0 {
				ctx := proxy.ContextWithAllowedUpstreamIDs(r.Context(), ids)
				r = r.WithContext(ctx)
			}

			next.ServeHTTP(w, r)
		})
	}
}
