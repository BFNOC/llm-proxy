package middleware

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Instawork/llm-proxy/internal/proxy"
)

// RequestClassifierMiddleware detects the provider style and extracts the
// downstream API key, storing both in the request context. Requests with no
// API key are rejected with 401.
func RequestClassifierMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			style := proxy.DetectProviderStyle(r)
			rawKey := proxy.ExtractDownstreamKey(r, style)

			if rawKey == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "missing API key"}) //nolint:errcheck
				return
			}

			keyHash := proxy.HashKey(rawKey)

			ctx := context.WithValue(r.Context(), ctxKeyStyle, style)
			ctx = context.WithValue(ctx, ctxKeyHash, keyHash)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
