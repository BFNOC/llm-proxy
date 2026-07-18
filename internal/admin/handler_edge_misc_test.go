package admin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/Instawork/llm-proxy/internal/middleware"
	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ======================== 12. getStatus edge cases ========================

func TestEdge_GetStatus_WithActiveUpstreams(t *testing.T) {
	h, router, _ := setupEdgeTestAdmin(t)

	// Set up active upstreams via dynamicProxy
	u, err := url.Parse("https://api.openai.com")
	require.NoError(t, err)
	h.dynamicProxy.SetAllUpstreams([]*proxy.ActiveUpstream{
		{
			ID:                1,
			Name:              "test-active-upstream",
			BaseURL:           u,
			APIKeys:           []string{"sk-test"},
			KeyRowIDs:         []int64{1},
			KeySchedulingMode: "round-robin",
		},
	})

	rr := doRequest(t, router, "GET", "/admin/api/status", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)

	healthyList := result["healthy_upstreams"].([]interface{})
	require.Len(t, healthyList, 1)
	upstream := healthyList[0].(map[string]interface{})
	assert.Equal(t, "test-active-upstream", upstream["name"])
	assert.Equal(t, "https://api.openai.com", upstream["url"])
	assert.Equal(t, float64(1), upstream["key_count"])
	assert.Equal(t, "round-robin", upstream["key_scheduling_mode"])

	// Verify other status fields are present
	assert.Contains(t, result, "version")
	assert.Equal(t, "test-edge", result["version"])
	assert.Contains(t, result, "active_requests")
	assert.Contains(t, result, "transport_pool")
}

func TestEdge_GetStatus_WithActiveUpstreams_EmptySchedulingMode(t *testing.T) {
	h, router, _ := setupEdgeTestAdmin(t)

	u, err := url.Parse("https://api.openai.com")
	require.NoError(t, err)
	h.dynamicProxy.SetAllUpstreams([]*proxy.ActiveUpstream{
		{
			ID:                2,
			Name:              "no-mode-upstream",
			BaseURL:           u,
			APIKeys:           []string{},
			KeySchedulingMode: "", // empty defaults to round-robin in getStatus
		},
	})

	rr := doRequest(t, router, "GET", "/admin/api/status", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)

	healthyList := result["healthy_upstreams"].([]interface{})
	require.Len(t, healthyList, 1)
	upstream := healthyList[0].(map[string]interface{})
	assert.Equal(t, "round-robin", upstream["key_scheduling_mode"], "empty scheduling mode should default to round-robin")
}

func TestEdge_GetStatus_NilRequestCounter(t *testing.T) {
	// Construct handler with nil requestCounter to cover the else branch
	dir := t.TempDir()
	encKey := []byte("01234567890123456789012345678901")
	s, err := store.NewStore(filepath.Join(dir, "nil-rc.db"), encKey)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	keyCache := middleware.NewKeyCache()
	keyCache.Reload(s)
	rateLimiter := middleware.NewPerKeyRPMLimiter()
	dp := proxy.NewDynamicProxy()
	overrideCache := middleware.NewModelOverrideCache(s)
	bindingCache := middleware.NewBindingCache(s)
	modelFilter := middleware.NewModelFilter(s)
	perKeyStats := middleware.NewPerKeyStatsCollector()
	headerCapture := middleware.NewHeaderCapture(20)
	prober := proxy.NewUpstreamProber(s, dp, 30*time.Second, 5*time.Second)

	// Pass nil for requestCounter
	h := NewAdminHandler(s, keyCache, rateLimiter, prober, dp, nil, modelFilter, nil, perKeyStats, overrideCache, bindingCache, headerCapture, nil, testAdminToken, "test-nil-rc")
	r := mux.NewRouter()
	h.RegisterRoutes(r)

	rr := doRequest(t, r, "GET", "/admin/api/status", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)
	assert.Equal(t, float64(0), result["rpm"])
	assert.Equal(t, "0.0", result["rps"])
}

// ======================== 13. serveDashboard ========================

func TestEdge_ServeDashboard_ReturnsHTML(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	// GET /admin/ should return dashboard HTML (no auth required for dashboard itself)
	req := httptest.NewRequest("GET", "/admin/", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rr.Body.String(), "<html", "response should contain HTML")
}

func TestEdge_ServeDashboard_SubPath(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	// Any /admin/* subpath (not matching /admin/api/ or /admin/assets/) should serve the dashboard shell
	req := httptest.NewRequest("GET", "/admin/some/path", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Type"), "text/html")
}

// ======================== 14. applyCFHeaders ========================

func TestEdge_ApplyCFHeaders(t *testing.T) {
	t.Run("both clearance and user agent", func(t *testing.T) {
		req := httptest.NewRequest("GET", "https://example.com", nil)
		applyCFHeaders(req, "test-clearance-value", "Mozilla/5.0")

		cookies := req.Cookies()
		require.Len(t, cookies, 1)
		assert.Equal(t, "cf_clearance", cookies[0].Name)
		assert.Equal(t, "test-clearance-value", cookies[0].Value)
		assert.Equal(t, "Mozilla/5.0", req.Header.Get("User-Agent"))
	})

	t.Run("empty clearance does not add cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "https://example.com", nil)
		applyCFHeaders(req, "", "Mozilla/5.0")

		cookies := req.Cookies()
		assert.Empty(t, cookies)
		assert.Equal(t, "Mozilla/5.0", req.Header.Get("User-Agent"))
	})

	t.Run("empty user agent does not set header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "https://example.com", nil)
		originalUA := req.Header.Get("User-Agent")
		applyCFHeaders(req, "clearance", "")

		assert.Equal(t, originalUA, req.Header.Get("User-Agent"))
		cookies := req.Cookies()
		require.Len(t, cookies, 1)
	})

	t.Run("both empty is no-op", func(t *testing.T) {
		req := httptest.NewRequest("GET", "https://example.com", nil)
		applyCFHeaders(req, "", "")

		cookies := req.Cookies()
		assert.Empty(t, cookies)
	})
}

// ======================== 15. assetsHandler ========================

func TestEdge_AssetsHandler_CSS(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	req := httptest.NewRequest("GET", "/admin/assets/admin.css", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	// Should have Cache-Control no-cache
	assert.Equal(t, "no-cache", rr.Header().Get("Cache-Control"))
	// Should contain CSS content
	assert.Contains(t, rr.Body.String(), "{", "CSS file should contain CSS rules")
}

func TestEdge_AssetsHandler_NotFound(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	req := httptest.NewRequest("GET", "/admin/assets/nonexistent.xyz", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestEdge_AssetsHandler_JS(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	req := httptest.NewRequest("GET", "/admin/assets/js/core.js", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "no-cache", rr.Header().Get("Cache-Control"))
}

// ======================== Additional edge cases ========================

func TestEdge_DeleteUpstream_InvalidID(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "DELETE", "/admin/api/upstreams/abc", nil)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEdge_DeleteKey_InvalidID(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "DELETE", "/admin/api/keys/abc", nil)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEdge_GetKeyUpstreams_NonexistentKey(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "GET", "/admin/api/keys/99999/upstreams", nil)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestEdge_GetKeyUpstreams_InvalidID(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "GET", "/admin/api/keys/abc/upstreams", nil)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEdge_GetKeyModelOverrides_NonexistentKey(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "GET", "/admin/api/keys/99999/model-overrides", nil)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestEdge_GetKeyModelOverrides_InvalidID(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "GET", "/admin/api/keys/abc/model-overrides", nil)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEdge_GetKeyModelOverrides_ValidEmptyResult(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	id := createEdgeKey(t, router, "no-overrides")
	path := fmt.Sprintf("/admin/api/keys/%v/model-overrides", id)

	rr := doRequest(t, router, "GET", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	// Should return empty array, not null
	assert.Contains(t, rr.Body.String(), "[]")
}

func TestEdge_GetAllKeyBindings(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "GET", "/admin/api/keys/bindings", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestEdge_GetAllKeyModelOverrides(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "GET", "/admin/api/keys/model-overrides", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	// Should not be null
	assert.NotContains(t, rr.Body.String(), "null")
}

func TestEdge_GetKeyRPM_NilPerKeyStats(t *testing.T) {
	dir := t.TempDir()
	encKey := []byte("01234567890123456789012345678901")
	s, err := store.NewStore(filepath.Join(dir, "nil-pks.db"), encKey)
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
	headerCapture := middleware.NewHeaderCapture(20)
	prober := proxy.NewUpstreamProber(s, dp, 30*time.Second, 5*time.Second)

	// Pass nil for perKeyStats
	h := NewAdminHandler(s, keyCache, rateLimiter, prober, dp, nil, modelFilter, globalCounter, nil, overrideCache, bindingCache, headerCapture, nil, testAdminToken, "test-nil-pks")
	r := mux.NewRouter()
	h.RegisterRoutes(r)

	rr := doRequest(t, r, "GET", "/admin/api/key-rpm", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	// Should return empty map
	assert.Contains(t, rr.Body.String(), "{}")
}

func TestEdge_ValidateBaseURL_Schemes(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"ftp scheme rejected", "ftp://example.com", true},
		{"empty scheme rejected", "://example.com", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBaseURL(tt.url)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEdge_ValidateProxyURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"http allowed", "http://proxy.example.com:8080", false},
		{"https allowed", "https://proxy.example.com:8080", false},
		{"socks5 allowed", "socks5://proxy.example.com:1080", false},
		{"ftp rejected", "ftp://proxy.example.com", true},
		{"empty hostname rejected", "http://", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProxyURL(tt.url)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEdge_SanitizeProxyForLog(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty returns empty", "", ""},
		{"strips credentials", "http://user:pass@proxy.com:8080", "http://proxy.com:8080"},
		{"no credentials unchanged", "http://proxy.com:8080", "http://proxy.com:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeProxyForLog(tt.raw)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEdge_CleanAPIKeys_Dedup(t *testing.T) {
	result := cleanAPIKeys([]string{"sk-a", "sk-b", "sk-a", "sk-c", "sk-b"})
	assert.Equal(t, []string{"sk-a", "sk-b", "sk-c"}, result)
}

func TestEdge_CleanAPIKeys_MultilineAndComma(t *testing.T) {
	result := cleanAPIKeys([]string{"sk-a\nsk-b\nsk-c"})
	assert.Equal(t, []string{"sk-a", "sk-b", "sk-c"}, result)

	result = cleanAPIKeys([]string{"sk-x,sk-y,sk-z"})
	assert.Equal(t, []string{"sk-x", "sk-y", "sk-z"}, result)
}

func TestEdge_NormalizeAPIKeyValues_TrimAndEmpty(t *testing.T) {
	result := normalizeAPIKeyValues("  sk-a  \n  \n  sk-b  ")
	assert.Equal(t, []string{"sk-a", "sk-b"}, result)

	result = normalizeAPIKeyValues("")
	assert.Empty(t, result)
}
