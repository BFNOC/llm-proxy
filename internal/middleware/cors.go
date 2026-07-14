package middleware

import "net/http"

// CORSMiddleware 为代理路由添加 CORS 头。
// 只应作用于 /v1/ 路由，不应用于 admin 路由。
func CORSMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, Cache-Control, x-api-key, anthropic-version")
			w.Header().Set("Access-Control-Expose-Headers", "Content-Type, Cache-Control")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
