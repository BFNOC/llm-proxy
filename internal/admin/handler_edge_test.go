package admin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"path/filepath"

	"github.com/Instawork/llm-proxy/internal/middleware"
	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
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

	h := NewAdminHandler(s, keyCache, rateLimiter, prober, dp, nil, modelFilter, globalCounter, perKeyStats, overrideCache, bindingCache, headerCapture, testAdminToken, "test-edge")
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

// ======================== 1. createUpstream edge cases ========================

func TestEdge_CreateUpstream_EmptyAPIKeysArray(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "POST", "/admin/api/upstreams", map[string]interface{}{
		"name":     "empty-keys",
		"base_url": "https://api.openai.com",
		"api_keys": []string{},
	})
	assert.Equal(t, http.StatusCreated, rr.Code)

	// Verify the upstream has no keys
	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	require.Len(t, list, 1)
	details := list[0]["api_key_details"].([]interface{})
	assert.Empty(t, details)
}

func TestEdge_CreateUpstream_WithRemark(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "POST", "/admin/api/upstreams", map[string]interface{}{
		"name":     "remarked",
		"base_url": "https://api.openai.com",
		"remark":   "This is a test remark for key sourcing",
	})
	assert.Equal(t, http.StatusCreated, rr.Code)

	// Verify remark appears in list
	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	require.Len(t, list, 1)
	assert.Equal(t, "This is a test remark for key sourcing", list[0]["remark"])
}

func TestEdge_CreateUpstream_OAuthWithKeys(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "POST", "/admin/api/upstreams", map[string]interface{}{
		"name":      "oauth-upstream",
		"base_url":  "https://api.openai.com",
		"auth_mode": "oauth",
		"api_keys":  []string{"oauth-token-1"},
	})
	assert.Equal(t, http.StatusCreated, rr.Code)

	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	list := decodeJSONArray(t, rr)
	require.Len(t, list, 1)
	assert.Equal(t, "oauth", list[0]["auth_mode"])
}

func TestEdge_CreateUpstream_InvalidJSON(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	req := httptest.NewRequest("POST", "/admin/api/upstreams", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	authRequest(req)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	result := decodeJSON(t, rr)
	assert.Contains(t, result["error"], "invalid JSON")
}

func TestEdge_CreateUpstream_FillSchedulingMode(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "POST", "/admin/api/upstreams", map[string]interface{}{
		"name":                "fill-upstream",
		"base_url":            "https://api.openai.com",
		"api_keys":            []string{"sk-k1", "sk-k2"},
		"key_scheduling_mode": "fill",
	})
	assert.Equal(t, http.StatusCreated, rr.Code)
}

// ======================== 2. updateUpstream edge cases ========================

func TestEdge_UpdateUpstream_BaseURL(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	id := createEdgeUpstream(t, router, "base-url-test", []string{"sk-k"})
	path := fmt.Sprintf("/admin/api/upstreams/%v", id)

	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"base_url": "https://api.anthropic.com",
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	// Verify via list
	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	list := decodeJSONArray(t, rr)
	require.Len(t, list, 1)
	assert.Equal(t, "https://api.anthropic.com", list[0]["base_url"])
}

func TestEdge_UpdateUpstream_InvalidBaseURL(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	id := createEdgeUpstream(t, router, "bad-base", []string{"sk-k"})
	path := fmt.Sprintf("/admin/api/upstreams/%v", id)

	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"base_url": "https://127.0.0.1",
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEdge_UpdateUpstream_ProxyURL(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	id := createEdgeUpstream(t, router, "proxy-test", nil)
	path := fmt.Sprintf("/admin/api/upstreams/%v", id)

	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"proxy_url": "socks5://proxy.example.com:1080",
	})
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestEdge_UpdateUpstream_InvalidProxyURL(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	id := createEdgeUpstream(t, router, "bad-proxy", nil)
	path := fmt.Sprintf("/admin/api/upstreams/%v", id)

	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"proxy_url": "ftp://bad.example.com",
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEdge_UpdateUpstream_Remark(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	id := createEdgeUpstream(t, router, "remark-update", nil)
	path := fmt.Sprintf("/admin/api/upstreams/%v", id)

	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"remark": "updated remark",
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	list := decodeJSONArray(t, rr)
	require.Len(t, list, 1)
	assert.Equal(t, "updated remark", list[0]["remark"])
}

func TestEdge_UpdateUpstream_APIKeysReplacement(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	id := createEdgeUpstream(t, router, "key-replace", []string{"sk-old1", "sk-old2"})
	path := fmt.Sprintf("/admin/api/upstreams/%v", id)

	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"api_keys": []string{"sk-new1", "sk-new2", "sk-new3"},
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	list := decodeJSONArray(t, rr)
	require.Len(t, list, 1)
	details := list[0]["api_key_details"].([]interface{})
	assert.Len(t, details, 3)
}

func TestEdge_UpdateUpstream_AuthMode(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	id := createEdgeUpstream(t, router, "auth-update", nil)
	path := fmt.Sprintf("/admin/api/upstreams/%v", id)

	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"auth_mode": "oauth",
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	list := decodeJSONArray(t, rr)
	require.Len(t, list, 1)
	assert.Equal(t, "oauth", list[0]["auth_mode"])
}

func TestEdge_UpdateUpstream_InvalidAuthMode(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	id := createEdgeUpstream(t, router, "bad-auth", nil)
	path := fmt.Sprintf("/admin/api/upstreams/%v", id)

	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"auth_mode": "magic",
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEdge_UpdateUpstream_InvalidJSON(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	id := createEdgeUpstream(t, router, "json-test", nil)
	path := fmt.Sprintf("/admin/api/upstreams/%v", id)

	req := httptest.NewRequest("PUT", path, strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	authRequest(req)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEdge_UpdateUpstream_InvalidID(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "PUT", "/admin/api/upstreams/abc", map[string]interface{}{
		"name": "nope",
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEdge_UpdateUpstream_BackwardCompatSingleKey(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	id := createEdgeUpstream(t, router, "compat-update", []string{"sk-original"})
	path := fmt.Sprintf("/admin/api/upstreams/%v", id)

	// Updating via the legacy api_key field should succeed
	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"api_key": "sk-replaced-via-single",
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	// Verify the upstream was updated (keys may or may not change depending on store behavior)
	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	list := decodeJSONArray(t, rr)
	require.Len(t, list, 1)
	assert.Equal(t, "compat-update", list[0]["name"])
}

// ======================== 3. createKey edge cases ========================

func TestEdge_CreateKey_InvalidJSON(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	req := httptest.NewRequest("POST", "/admin/api/keys", strings.NewReader("%%%"))
	req.Header.Set("Content-Type", "application/json")
	authRequest(req)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	result := decodeJSON(t, rr)
	assert.Contains(t, result["error"], "invalid JSON")
}

func TestEdge_CreateKey_VeryLongName(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	longName := strings.Repeat("a", 500)
	rr := doRequest(t, router, "POST", "/admin/api/keys", map[string]interface{}{
		"name": longName,
	})
	assert.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	assert.Equal(t, longName, created["name"])
}

// ======================== 4. revealKey edge cases ========================

func TestEdge_RevealKey_InvalidIDFormat(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "GET", "/admin/api/keys/abc/reveal", nil)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ======================== 5. listKeys edge cases ========================

func TestEdge_ListKeys_MultipleWithDifferentEnabledStates(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	// Create two keys
	id1 := createEdgeKey(t, router, "enabled-key")
	id2 := createEdgeKey(t, router, "disabled-key")

	// Disable the second key
	path := fmt.Sprintf("/admin/api/keys/%v", id2)
	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"enabled": false,
	})
	require.Equal(t, http.StatusOK, rr.Code)

	// List and verify states
	rr = doRequest(t, router, "GET", "/admin/api/keys", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	assert.Len(t, list, 2)

	// Find each key by ID and check enabled state
	enabledStates := map[float64]bool{}
	for _, k := range list {
		enabledStates[k["id"].(float64)] = k["enabled"].(bool)
	}
	assert.True(t, enabledStates[id1])
	assert.False(t, enabledStates[id2])
}

// ======================== 6. updateKey edge cases ========================

func TestEdge_UpdateKey_ToggleEnabled(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	id := createEdgeKey(t, router, "toggle-key")
	path := fmt.Sprintf("/admin/api/keys/%v", id)

	// Disable
	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"enabled": false,
	})
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)
	assert.Equal(t, false, result["enabled"])

	// Re-enable
	rr = doRequest(t, router, "PUT", path, map[string]interface{}{
		"enabled": true,
	})
	assert.Equal(t, http.StatusOK, rr.Code)
	result = decodeJSON(t, rr)
	assert.Equal(t, true, result["enabled"])
}

func TestEdge_UpdateKey_InvalidJSON(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	id := createEdgeKey(t, router, "json-key")
	path := fmt.Sprintf("/admin/api/keys/%v", id)

	req := httptest.NewRequest("PUT", path, strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	authRequest(req)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEdge_UpdateKey_InvalidID(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "PUT", "/admin/api/keys/abc", map[string]interface{}{
		"name": "nope",
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ======================== 7. setKeyUpstreams edge cases ========================

func TestEdge_SetKeyUpstreams_InvalidJSON(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	id := createEdgeKey(t, router, "binding-key")
	path := fmt.Sprintf("/admin/api/keys/%v/upstreams", id)

	req := httptest.NewRequest("PUT", path, strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	authRequest(req)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEdge_SetKeyUpstreams_NonexistentKey(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "PUT", "/admin/api/keys/99999/upstreams", map[string]interface{}{
		"upstream_ids": []int64{},
	})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestEdge_SetKeyUpstreams_NonexistentUpstream(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	id := createEdgeKey(t, router, "bind-nonexist")
	path := fmt.Sprintf("/admin/api/keys/%v/upstreams", id)

	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"upstream_ids": []int64{99999},
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	result := decodeJSON(t, rr)
	assert.Contains(t, result["error"], "not found")
}

func TestEdge_SetKeyUpstreams_EmptyArrayClearsBindings(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	keyID := createEdgeKey(t, router, "clear-bindings")
	upID := createEdgeUpstream(t, router, "bind-target", nil)

	// Bind
	path := fmt.Sprintf("/admin/api/keys/%v/upstreams", keyID)
	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"upstream_ids": []float64{upID},
	})
	require.Equal(t, http.StatusOK, rr.Code)

	// Clear bindings with empty array
	rr = doRequest(t, router, "PUT", path, map[string]interface{}{
		"upstream_ids": []int64{},
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	// Verify cleared
	rr = doRequest(t, router, "GET", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)
	ids := result["upstream_ids"].([]interface{})
	assert.Empty(t, ids)
}

func TestEdge_SetKeyUpstreams_DuplicateUpstreamIDs(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	keyID := createEdgeKey(t, router, "dedup-key")
	upID := createEdgeUpstream(t, router, "dedup-up", nil)

	// Send duplicate IDs -- handler deduplicates
	path := fmt.Sprintf("/admin/api/keys/%v/upstreams", keyID)
	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"upstream_ids": []float64{upID, upID, upID},
	})
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)
	ids := result["upstream_ids"].([]interface{})
	assert.Len(t, ids, 1, "duplicates should be deduplicated")
}

func TestEdge_SetKeyUpstreams_InvalidID(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "PUT", "/admin/api/keys/abc/upstreams", map[string]interface{}{
		"upstream_ids": []int64{},
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ======================== 8. setKeyModelOverrides edge cases ========================

func TestEdge_SetKeyModelOverrides_InvalidGlobPattern(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	keyID := createEdgeKey(t, router, "override-key")
	upID := createEdgeUpstream(t, router, "override-up", nil)

	path := fmt.Sprintf("/admin/api/keys/%v/model-overrides", keyID)
	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"model_pattern": "[invalid", "upstream_id": upID},
		},
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	result := decodeJSON(t, rr)
	assert.Contains(t, result["error"], "invalid pattern")
}

func TestEdge_SetKeyModelOverrides_DuplicatePatternUpstreamCombo(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	keyID := createEdgeKey(t, router, "dup-override-key")
	upID := createEdgeUpstream(t, router, "dup-override-up", nil)

	path := fmt.Sprintf("/admin/api/keys/%v/model-overrides", keyID)
	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"model_pattern": "gpt-*", "upstream_id": upID},
			{"model_pattern": "gpt-*", "upstream_id": upID},
		},
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	result := decodeJSON(t, rr)
	assert.Contains(t, result["error"], "duplicate")
}

func TestEdge_SetKeyModelOverrides_EmptyModelPattern(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	keyID := createEdgeKey(t, router, "empty-pat-key")
	upID := createEdgeUpstream(t, router, "empty-pat-up", nil)

	path := fmt.Sprintf("/admin/api/keys/%v/model-overrides", keyID)
	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"model_pattern": "", "upstream_id": upID},
		},
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	result := decodeJSON(t, rr)
	assert.Contains(t, result["error"], "model_pattern is required")
}

func TestEdge_SetKeyModelOverrides_NonexistentUpstream(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	keyID := createEdgeKey(t, router, "bad-up-key")
	path := fmt.Sprintf("/admin/api/keys/%v/model-overrides", keyID)

	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"model_pattern": "gpt-*", "upstream_id": 99999},
		},
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	result := decodeJSON(t, rr)
	assert.Contains(t, result["error"], "upstream 99999 not found")
}

func TestEdge_SetKeyModelOverrides_NonexistentKey(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "PUT", "/admin/api/keys/99999/model-overrides", map[string]interface{}{
		"overrides": []map[string]interface{}{},
	})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestEdge_SetKeyModelOverrides_InvalidJSON(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	keyID := createEdgeKey(t, router, "json-override-key")
	path := fmt.Sprintf("/admin/api/keys/%v/model-overrides", keyID)

	req := httptest.NewRequest("PUT", path, strings.NewReader("bad json"))
	req.Header.Set("Content-Type", "application/json")
	authRequest(req)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEdge_SetKeyModelOverrides_EmptyArrayClears(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	keyID := createEdgeKey(t, router, "clear-override-key")
	upID := createEdgeUpstream(t, router, "clear-override-up", nil)

	path := fmt.Sprintf("/admin/api/keys/%v/model-overrides", keyID)

	// Set some overrides
	rr := doRequest(t, router, "PUT", path, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"model_pattern": "gpt-*", "upstream_id": upID},
		},
	})
	require.Equal(t, http.StatusOK, rr.Code)

	// Clear with empty array
	rr = doRequest(t, router, "PUT", path, map[string]interface{}{
		"overrides": []map[string]interface{}{},
	})
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)
	assert.Equal(t, float64(0), result["count"])
}

// ======================== 9. addModelWhitelist edge cases ========================

func TestEdge_AddModelWhitelist_InvalidGlobPattern(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "POST", "/admin/api/models/whitelist", map[string]interface{}{
		"pattern": "[invalid",
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	result := decodeJSON(t, rr)
	assert.Contains(t, result["error"], "invalid pattern")
}

func TestEdge_AddModelWhitelist_EmptyPattern(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "POST", "/admin/api/models/whitelist", map[string]interface{}{
		"pattern": "",
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	result := decodeJSON(t, rr)
	assert.Contains(t, result["error"], "pattern is required")
}

func TestEdge_AddModelWhitelist_ValidPattern(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "POST", "/admin/api/models/whitelist", map[string]interface{}{
		"pattern": "gpt-4*",
	})
	assert.Equal(t, http.StatusCreated, rr.Code)
	result := decodeJSON(t, rr)
	// ModelWhitelistEntry has no JSON tags; fields serialize as uppercase
	assert.Equal(t, "gpt-4*", result["Pattern"])

	// List to confirm
	rr = doRequest(t, router, "GET", "/admin/api/models/whitelist", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	assert.Len(t, list, 1)
}

func TestEdge_AddModelWhitelist_InvalidJSON(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	req := httptest.NewRequest("POST", "/admin/api/models/whitelist", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	authRequest(req)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ======================== 10. deleteModelWhitelist edge cases ========================

func TestEdge_DeleteModelWhitelist_InvalidIDFormat(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "DELETE", "/admin/api/models/whitelist/abc", nil)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEdge_DeleteModelWhitelist_ValidDelete(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	// Create a whitelist entry
	rr := doRequest(t, router, "POST", "/admin/api/models/whitelist", map[string]interface{}{
		"pattern": "claude-*",
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	// ModelWhitelistEntry has no JSON tags; ID serializes as uppercase
	id := created["ID"]

	// Delete it
	path := fmt.Sprintf("/admin/api/models/whitelist/%v", id)
	rr = doRequest(t, router, "DELETE", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)
	assert.Equal(t, "deleted", result["status"])
}

// ======================== 11. batchDeleteModelWhitelist edge cases ========================

func TestEdge_BatchDeleteModelWhitelist_InvalidJSON(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	req := httptest.NewRequest("DELETE", "/admin/api/models/whitelist/batch", strings.NewReader("{bad}"))
	req.Header.Set("Content-Type", "application/json")
	authRequest(req)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEdge_BatchDeleteModelWhitelist_EmptyIDs(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	rr := doRequest(t, router, "DELETE", "/admin/api/models/whitelist/batch", map[string]interface{}{
		"ids": []int64{},
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	result := decodeJSON(t, rr)
	assert.Contains(t, result["error"], "ids is required")
}

func TestEdge_BatchDeleteModelWhitelist_ValidBatch(t *testing.T) {
	_, router, _ := setupEdgeTestAdmin(t)

	// Create two entries
	var ids []float64
	for _, pat := range []string{"gpt-*", "claude-*"} {
		rr := doRequest(t, router, "POST", "/admin/api/models/whitelist", map[string]interface{}{
			"pattern": pat,
		})
		require.Equal(t, http.StatusCreated, rr.Code)
		created := decodeJSON(t, rr)
		// ModelWhitelistEntry has no JSON tags; ID serializes as uppercase
		ids = append(ids, created["ID"].(float64))
	}

	// Batch delete
	rr := doRequest(t, router, "DELETE", "/admin/api/models/whitelist/batch", map[string]interface{}{
		"ids": ids,
	})
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)
	assert.Equal(t, "deleted", result["status"])
	assert.Equal(t, float64(2), result["deleted"])
}

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
	h := NewAdminHandler(s, keyCache, rateLimiter, prober, dp, nil, modelFilter, nil, perKeyStats, overrideCache, bindingCache, headerCapture, testAdminToken, "test-nil-rc")
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
	h := NewAdminHandler(s, keyCache, rateLimiter, prober, dp, nil, modelFilter, globalCounter, nil, overrideCache, bindingCache, headerCapture, testAdminToken, "test-nil-pks")
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
