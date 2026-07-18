package admin

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Instawork/llm-proxy/internal/middleware"
	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

// setupEdgeTestAdmin creates a fresh AdminHandler + router for edge-case tests.
// It mirrors setupTestAdmin but uses its own name to avoid collisions.
func setupEdgeTestAdmin(t *testing.T) (*AdminHandler, *mux.Router, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	encKey := []byte("01234567890123456789012345678901") // 32 bytes
	s, err := store.NewStore(filepath.Join(dir, "edge.db"), encKey)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	keyCache := middleware.NewKeyCache()
	keyCache.Reload(s)
	rateLimiter := middleware.NewPerKeyRPMLimiter()
	dp := proxy.NewDynamicProxy()
	overrideCache := middleware.NewModelOverrideCache(s)
	bindingCache := middleware.NewBindingCache(s)
	modelFilter := middleware.NewModelFilter(s)
	globalCounter := middleware.NewGlobalRequestCounter()
	perKeyStats := middleware.NewPerKeyStatsCollector()
	headerCapture := middleware.NewHeaderCapture(20)

	prober := proxy.NewUpstreamProber(s, dp, 30*time.Second, 5*time.Second)

	h := NewAdminHandler(s, keyCache, rateLimiter, prober, dp, nil, modelFilter, globalCounter, perKeyStats, overrideCache, bindingCache, headerCapture, nil, testAdminToken, "test-edge")
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	return h, r, s
}

// createEdgeUpstream is a helper to create an upstream and return its ID.
func createEdgeUpstream(t *testing.T, router *mux.Router, name string, keys []string) float64 {
	t.Helper()
	body := map[string]interface{}{
		"name":     name,
		"base_url": "https://api.openai.com",
	}
	if keys != nil {
		body["api_keys"] = keys
	}
	rr := doRequest(t, router, "POST", "/admin/api/upstreams", body)
	require.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	return created["id"].(float64)
}

// createEdgeKey is a helper to create a downstream key and return its ID.
func createEdgeKey(t *testing.T, router *mux.Router, name string) float64 {
	t.Helper()
	rr := doRequest(t, router, "POST", "/admin/api/keys", map[string]interface{}{
		"name": name,
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	return created["id"].(float64)
}
