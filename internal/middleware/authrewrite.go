package middleware

import (
	"net/http"

	"github.com/Instawork/llm-proxy/internal/proxy"
)

// AuthRewriteMiddleware replaces the downstream API key in auth headers with
// the active upstream's key before forwarding.
func AuthRewriteMiddleware(dp *proxy.DynamicProxy) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			active := dp.GetActiveUpstream()
			if active != nil {
				style := StyleFromContext(r.Context())
				proxy.RewriteAuthHeaders(r, style, active.APIKey)
			}
			next.ServeHTTP(w, r)
		})
	}
}
