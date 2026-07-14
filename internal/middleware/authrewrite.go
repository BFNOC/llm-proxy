package middleware

import (
	"net/http"

	"github.com/Instawork/llm-proxy/internal/proxy"
)

// AuthRewriteMiddleware 在转发前，用当前活跃上游的 Key 替换鉴权头中的下游 API Key。
func AuthRewriteMiddleware(dp *proxy.DynamicProxy) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			active := dp.GetActiveUpstream()
			if active != nil {
				style := StyleFromContext(r.Context())
				key, _, _ := active.NextAPIKey()
				proxy.RewriteAuthHeaders(r, style, key, active.AuthMode)
			}
			next.ServeHTTP(w, r)
		})
	}
}
