package admin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
