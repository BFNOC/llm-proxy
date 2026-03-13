package middleware

import "net/http"

// CORSMiddleware adds CORS headers for the proxy routes.
// This should only be applied to /v1/ routes, not admin routes.
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
