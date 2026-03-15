package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
)

// UpstreamBindingMiddleware looks up the bound upstream IDs for the
// resolved downstream key and stores them in the request context.
// DynamicProxy reads these IDs to filter the upstream list per-request.
// If no bindings exist the context value is nil, meaning all upstreams.
// On store errors, the request is rejected (fail-closed) to prevent
// bound keys from bypassing upstream restrictions.
func UpstreamBindingMiddleware(s *store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			keyID := DownstreamKeyIDFromContext(r.Context())
			if keyID == 0 {
				next.ServeHTTP(w, r)
				return
			}

			ids, err := s.GetKeyUpstreamIDs(keyID)
			if err != nil {
				slog.Error("binding: failed to get key upstream bindings", "key_id", keyID, "error", err)
				// Fail closed: reject request on store error to prevent bypass
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				json.NewEncoder(w).Encode(map[string]string{"error": "upstream binding lookup failed"}) //nolint:errcheck
				return
			}

			if len(ids) > 0 {
				ctx := proxy.ContextWithAllowedUpstreamIDs(r.Context(), ids)
				r = r.WithContext(ctx)
			}

			next.ServeHTTP(w, r)
		})
	}
}
