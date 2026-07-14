package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

const testAdminToken = "test-token"

func setupTestAdmin(t *testing.T) (*AdminHandler, *mux.Router) {
	t.Helper()
	dir := t.TempDir()
	encKey := []byte("01234567890123456789012345678901") // 32 bytes
	s, err := store.NewStore(filepath.Join(dir, "test.db"), encKey)
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

	h := NewAdminHandler(s, keyCache, rateLimiter, prober, dp, nil, modelFilter, globalCounter, perKeyStats, overrideCache, bindingCache, headerCapture, testAdminToken, "test")
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	return h, r
}

// authRequest adds the admin token header to a request.
func authRequest(req *http.Request) *http.Request {
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	return req
}

// doRequest is a helper that executes a request through the router and returns the recorder.
func doRequest(t *testing.T, router *mux.Router, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	authRequest(req)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

// decodeJSON decodes the response body into a map.
func decodeJSON(t *testing.T, rr *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	err := json.Unmarshal(rr.Body.Bytes(), &result)
	require.NoError(t, err, "response body: %s", rr.Body.String())
	return result
}

// decodeJSONArray decodes the response body into a slice of maps.
func decodeJSONArray(t *testing.T, rr *httptest.ResponseRecorder) []map[string]interface{} {
	t.Helper()
	var result []map[string]interface{}
	err := json.Unmarshal(rr.Body.Bytes(), &result)
	require.NoError(t, err, "response body: %s", rr.Body.String())
	return result
}

// --- Auth Middleware Tests ---

func TestAuthMiddleware(t *testing.T) {
	_, router := setupTestAdmin(t)

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{
			name:       "missing token returns 401",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong token returns 401",
			authHeader: "Bearer wrong-token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing Bearer prefix returns 401",
			authHeader: testAdminToken,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "valid token returns 200",
			authHeader: "Bearer " + testAdminToken,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/admin/api/upstreams", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

// --- Upstream CRUD Tests ---

func TestListUpstreams_Empty(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSONArray(t, rr)
	assert.Empty(t, result)
}

func TestCreateUpstream(t *testing.T) {
	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
		wantError  bool
	}{
		{
			name: "valid create with api_keys",
			body: map[string]interface{}{
				"name":     "test-upstream",
				"base_url": "https://api.openai.com",
				"api_keys": []string{"sk-test-key-1", "sk-test-key-2"},
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "valid create without api_keys (public upstream)",
			body: map[string]interface{}{
				"name":     "public-upstream",
				"base_url": "https://api.openai.com",
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "missing name returns 400",
			body: map[string]interface{}{
				"base_url": "https://api.openai.com",
			},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
		{
			name: "missing base_url returns 400",
			body: map[string]interface{}{
				"name": "test-upstream",
			},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
		{
			name: "invalid scheduling mode returns 400",
			body: map[string]interface{}{
				"name":                "test-upstream",
				"base_url":            "https://api.openai.com",
				"key_scheduling_mode": "invalid",
			},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
		{
			name: "invalid auth_mode returns 400",
			body: map[string]interface{}{
				"name":      "test-upstream",
				"base_url":  "https://api.openai.com",
				"auth_mode": "invalid",
			},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
		{
			name: "SSRF loopback base_url returns 400",
			body: map[string]interface{}{
				"name":     "ssrf-upstream",
				"base_url": "https://127.0.0.1",
			},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
		{
			name: "invalid proxy_url scheme returns 400",
			body: map[string]interface{}{
				"name":      "test-upstream",
				"base_url":  "https://api.openai.com",
				"proxy_url": "ftp://proxy.example.com",
			},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, router := setupTestAdmin(t)
			rr := doRequest(t, router, "POST", "/admin/api/upstreams", tt.body)
			assert.Equal(t, tt.wantStatus, rr.Code)
			if tt.wantError {
				result := decodeJSON(t, rr)
				assert.Contains(t, result, "error")
			}
		})
	}
}

func TestCreateUpstream_ThenList(t *testing.T) {
	_, router := setupTestAdmin(t)

	// Create
	rr := doRequest(t, router, "POST", "/admin/api/upstreams", map[string]interface{}{
		"name":     "openai",
		"base_url": "https://api.openai.com",
		"api_keys": []string{"sk-key1"},
	})
	assert.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	assert.NotNil(t, created["id"])

	// List
	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	assert.Len(t, list, 1)
	assert.Equal(t, "openai", list[0]["name"])
	assert.Equal(t, "https://api.openai.com", list[0]["base_url"])
}

func TestUpdateUpstream(t *testing.T) {
	_, router := setupTestAdmin(t)

	// Create upstream first
	rr := doRequest(t, router, "POST", "/admin/api/upstreams", map[string]interface{}{
		"name":     "original",
		"base_url": "https://api.openai.com",
		"api_keys": []string{"sk-key1"},
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	id := created["id"]

	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
		checkFn    func(t *testing.T, result map[string]interface{})
	}{
		{
			name: "update name",
			body: map[string]interface{}{
				"name": "updated-name",
			},
			wantStatus: http.StatusOK,
			checkFn: func(t *testing.T, result map[string]interface{}) {
				assert.Equal(t, "updated-name", result["name"])
			},
		},
		{
			name: "update enabled to false",
			body: map[string]interface{}{
				"enabled": false,
			},
			wantStatus: http.StatusOK,
			checkFn: func(t *testing.T, result map[string]interface{}) {
				assert.Equal(t, false, result["enabled"])
			},
		},
		{
			name: "update enabled back to true",
			body: map[string]interface{}{
				"enabled": true,
			},
			wantStatus: http.StatusOK,
			checkFn: func(t *testing.T, result map[string]interface{}) {
				assert.Equal(t, true, result["enabled"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := fmt.Sprintf("/admin/api/upstreams/%v", id)
			rr := doRequest(t, router, "PUT", path, tt.body)
			assert.Equal(t, tt.wantStatus, rr.Code)
			if tt.checkFn != nil {
				result := decodeJSON(t, rr)
				tt.checkFn(t, result)
			}
		})
	}
}

func TestUpdateUpstream_NotFound(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequest(t, router, "PUT", "/admin/api/upstreams/99999", map[string]interface{}{
		"name": "nope",
	})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestDeleteUpstream(t *testing.T) {
	_, router := setupTestAdmin(t)

	// Create
	rr := doRequest(t, router, "POST", "/admin/api/upstreams", map[string]interface{}{
		"name":     "to-delete",
		"base_url": "https://api.openai.com",
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	id := created["id"]

	// Delete
	path := fmt.Sprintf("/admin/api/upstreams/%v", id)
	rr = doRequest(t, router, "DELETE", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)
	assert.Equal(t, "deleted", result["status"])

	// Verify list is empty
	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	assert.Empty(t, list)
}

func TestDeleteUpstream_NotFound(t *testing.T) {
	_, router := setupTestAdmin(t)

	// The store returns an error when affected rows == 0 for a non-existent upstream,
	// and the handler maps store errors to 500. This is the current behavior.
	rr := doRequest(t, router, "DELETE", "/admin/api/upstreams/99999", nil)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// --- Batch Upstream Operations ---

func TestBatchSetUpstreamEnabled(t *testing.T) {
	_, router := setupTestAdmin(t)

	// Create two upstreams
	var ids []float64
	for _, name := range []string{"up1", "up2"} {
		rr := doRequest(t, router, "POST", "/admin/api/upstreams", map[string]interface{}{
			"name":     name,
			"base_url": "https://api.openai.com",
		})
		require.Equal(t, http.StatusCreated, rr.Code)
		created := decodeJSON(t, rr)
		ids = append(ids, created["id"].(float64))
	}

	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
	}{
		{
			name: "disable both",
			body: map[string]interface{}{
				"ids":     ids,
				"enabled": false,
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "enable both",
			body: map[string]interface{}{
				"ids":     ids,
				"enabled": true,
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "missing ids returns 400",
			body: map[string]interface{}{
				"enabled": true,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "missing enabled returns 400",
			body: map[string]interface{}{
				"ids": ids,
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := doRequest(t, router, "PUT", "/admin/api/upstreams/batch/enabled", tt.body)
			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

func TestBatchDeleteUpstreams(t *testing.T) {
	_, router := setupTestAdmin(t)

	// Create two upstreams
	var ids []float64
	for _, name := range []string{"del1", "del2"} {
		rr := doRequest(t, router, "POST", "/admin/api/upstreams", map[string]interface{}{
			"name":     name,
			"base_url": "https://api.openai.com",
		})
		require.Equal(t, http.StatusCreated, rr.Code)
		created := decodeJSON(t, rr)
		ids = append(ids, created["id"].(float64))
	}

	// Batch delete
	rr := doRequest(t, router, "DELETE", "/admin/api/upstreams/batch", map[string]interface{}{
		"ids": ids,
	})
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)
	assert.Equal(t, "deleted", result["status"])
	assert.Equal(t, float64(2), result["deleted"])

	// Verify list is empty
	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	assert.Empty(t, list)
}

func TestBatchDeleteUpstreams_EmptyIDs(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequest(t, router, "DELETE", "/admin/api/upstreams/batch", map[string]interface{}{
		"ids": []int64{},
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// --- Key CRUD Tests ---

func TestCreateKey(t *testing.T) {
	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
		wantError  bool
	}{
		{
			name: "valid create",
			body: map[string]interface{}{
				"name":      "test-key",
				"rpm_limit": 100,
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "valid create with zero rpm_limit (unlimited)",
			body: map[string]interface{}{
				"name":      "unlimited-key",
				"rpm_limit": 0,
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "missing name returns 400",
			body: map[string]interface{}{
				"rpm_limit": 100,
			},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, router := setupTestAdmin(t)
			rr := doRequest(t, router, "POST", "/admin/api/keys", tt.body)
			assert.Equal(t, tt.wantStatus, rr.Code)
			if !tt.wantError {
				result := decodeJSON(t, rr)
				// Plaintext key returned once on create
				assert.NotEmpty(t, result["key"], "plaintext key must be returned on create")
				assert.NotNil(t, result["id"])
				assert.Equal(t, tt.body["name"], result["name"])
			}
		})
	}
}

func TestCreateKey_PlaintextReturnedOnce(t *testing.T) {
	_, router := setupTestAdmin(t)

	// Create key
	rr := doRequest(t, router, "POST", "/admin/api/keys", map[string]interface{}{
		"name":      "once-key",
		"rpm_limit": 50,
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	plaintext := created["key"].(string)
	assert.True(t, len(plaintext) > 0, "plaintext key should be non-empty")

	// List keys — no plaintext should appear
	rr = doRequest(t, router, "GET", "/admin/api/keys", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	require.Len(t, list, 1)
	_, hasKey := list[0]["key"]
	assert.False(t, hasKey, "list endpoint should not return plaintext key")
	assert.NotEmpty(t, list[0]["key_prefix"], "list should return key_prefix")
}

func TestListKeys_Empty(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequest(t, router, "GET", "/admin/api/keys", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	assert.Empty(t, list)
}

func TestListKeys_AfterCreate(t *testing.T) {
	_, router := setupTestAdmin(t)

	// Create two keys
	for _, name := range []string{"key-a", "key-b"} {
		rr := doRequest(t, router, "POST", "/admin/api/keys", map[string]interface{}{
			"name": name,
		})
		require.Equal(t, http.StatusCreated, rr.Code)
	}

	// List
	rr := doRequest(t, router, "GET", "/admin/api/keys", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	assert.Len(t, list, 2)
}

func TestUpdateKey(t *testing.T) {
	_, router := setupTestAdmin(t)

	// Create key
	rr := doRequest(t, router, "POST", "/admin/api/keys", map[string]interface{}{
		"name":      "original-key",
		"rpm_limit": 100,
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	id := created["id"]

	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
		checkFn    func(t *testing.T, result map[string]interface{})
	}{
		{
			name: "update name",
			body: map[string]interface{}{
				"name": "renamed-key",
			},
			wantStatus: http.StatusOK,
			checkFn: func(t *testing.T, result map[string]interface{}) {
				assert.Equal(t, "renamed-key", result["name"])
			},
		},
		{
			name: "update rpm_limit",
			body: map[string]interface{}{
				"rpm_limit": 200,
			},
			wantStatus: http.StatusOK,
			checkFn: func(t *testing.T, result map[string]interface{}) {
				assert.Equal(t, float64(200), result["rpm_limit"])
			},
		},
		{
			name: "update enabled to false",
			body: map[string]interface{}{
				"enabled": false,
			},
			wantStatus: http.StatusOK,
			checkFn: func(t *testing.T, result map[string]interface{}) {
				assert.Equal(t, false, result["enabled"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := fmt.Sprintf("/admin/api/keys/%v", id)
			rr := doRequest(t, router, "PUT", path, tt.body)
			assert.Equal(t, tt.wantStatus, rr.Code)
			if tt.checkFn != nil {
				result := decodeJSON(t, rr)
				tt.checkFn(t, result)
			}
		})
	}
}

func TestUpdateKey_NotFound(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequest(t, router, "PUT", "/admin/api/keys/99999", map[string]interface{}{
		"name": "nope",
	})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestDeleteKey(t *testing.T) {
	_, router := setupTestAdmin(t)

	// Create key
	rr := doRequest(t, router, "POST", "/admin/api/keys", map[string]interface{}{
		"name": "to-delete-key",
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	id := created["id"]

	// Delete
	path := fmt.Sprintf("/admin/api/keys/%v", id)
	rr = doRequest(t, router, "DELETE", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)
	assert.Equal(t, "deleted", result["status"])

	// Verify list is empty
	rr = doRequest(t, router, "GET", "/admin/api/keys", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	assert.Empty(t, list)
}

func TestDeleteKey_NotFound(t *testing.T) {
	_, router := setupTestAdmin(t)

	// The store returns an error when affected rows == 0 for a non-existent key,
	// and the handler maps store errors to 500. This is the current behavior.
	rr := doRequest(t, router, "DELETE", "/admin/api/keys/99999", nil)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// --- Upstream Update with API Keys ---

func TestUpdateUpstream_UpdateAPIKeys(t *testing.T) {
	_, router := setupTestAdmin(t)

	// Create upstream with initial keys
	rr := doRequest(t, router, "POST", "/admin/api/upstreams", map[string]interface{}{
		"name":     "keyed-upstream",
		"base_url": "https://api.openai.com",
		"api_keys": []string{"sk-initial"},
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	id := created["id"]

	// Update with new keys (full replacement)
	path := fmt.Sprintf("/admin/api/upstreams/%v", id)
	rr = doRequest(t, router, "PUT", path, map[string]interface{}{
		"api_keys": []string{"sk-new-1", "sk-new-2"},
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	// Verify via list
	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	require.Len(t, list, 1)
	details := list[0]["api_key_details"].([]interface{})
	assert.Len(t, details, 2)
}

// --- Status Endpoint ---

func TestGetStatus(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequest(t, router, "GET", "/admin/api/status", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)

	// Verify essential fields exist
	assert.Contains(t, result, "healthy_upstreams")
	assert.Contains(t, result, "total_keys")
	assert.Contains(t, result, "today_requests")
	assert.Contains(t, result, "uptime")
	assert.Contains(t, result, "version")
	assert.Equal(t, "test", result["version"])
	assert.Contains(t, result, "rpm")
	assert.Contains(t, result, "rps")
}

// --- Create Upstream with scheduling mode and auth mode ---

func TestCreateUpstream_SchedulingAndAuthModes(t *testing.T) {
	tests := []struct {
		name             string
		schedulingMode   string
		authMode         string
		wantStatus       int
	}{
		{
			name:           "round-robin scheduling",
			schedulingMode: "round-robin",
			authMode:       "api_key",
			wantStatus:     http.StatusCreated,
		},
		{
			name:           "fill scheduling",
			schedulingMode: "fill",
			authMode:       "api_key",
			wantStatus:     http.StatusCreated,
		},
		{
			name:           "oauth auth mode",
			schedulingMode: "round-robin",
			authMode:       "oauth",
			wantStatus:     http.StatusCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, router := setupTestAdmin(t)
			rr := doRequest(t, router, "POST", "/admin/api/upstreams", map[string]interface{}{
				"name":                "mode-test",
				"base_url":            "https://api.openai.com",
				"key_scheduling_mode": tt.schedulingMode,
				"auth_mode":           tt.authMode,
			})
			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

// --- Upstream create with backward-compatible single api_key ---

func TestCreateUpstream_BackwardCompatSingleKey(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequest(t, router, "POST", "/admin/api/upstreams", map[string]interface{}{
		"name":     "compat-upstream",
		"base_url": "https://api.openai.com",
		"api_key":  "sk-compat-key",
	})
	assert.Equal(t, http.StatusCreated, rr.Code)

	// Verify key is stored
	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	require.Len(t, list, 1)
	details := list[0]["api_key_details"].([]interface{})
	require.Len(t, details, 1)
	detail := details[0].(map[string]interface{})
	assert.Equal(t, "sk-compat-key", detail["key"])
}

// --- Update upstream with invalid scheduling mode ---

func TestUpdateUpstream_InvalidSchedulingMode(t *testing.T) {
	_, router := setupTestAdmin(t)

	// Create upstream
	rr := doRequest(t, router, "POST", "/admin/api/upstreams", map[string]interface{}{
		"name":     "sched-test",
		"base_url": "https://api.openai.com",
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	id := created["id"]

	// Update with invalid mode
	path := fmt.Sprintf("/admin/api/upstreams/%v", id)
	rr = doRequest(t, router, "PUT", path, map[string]interface{}{
		"key_scheduling_mode": "random",
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// --- Key RPM endpoint ---

func TestGetKeyRPM(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequest(t, router, "GET", "/admin/api/key-rpm", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	// Should return an empty map initially
	result := decodeJSON(t, rr)
	assert.NotNil(t, result)
}

// --- Reveal Key ---

func TestRevealKey(t *testing.T) {
	_, router := setupTestAdmin(t)

	// Create key
	rr := doRequest(t, router, "POST", "/admin/api/keys", map[string]interface{}{
		"name": "reveal-test",
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	id := created["id"]
	originalPlaintext := created["key"].(string)

	// Reveal the key
	path := fmt.Sprintf("/admin/api/keys/%v/reveal", id)
	rr = doRequest(t, router, "GET", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	result := decodeJSON(t, rr)
	assert.Equal(t, originalPlaintext, result["key"])
}

func TestRevealKey_NotFound(t *testing.T) {
	_, router := setupTestAdmin(t)

	rr := doRequest(t, router, "GET", "/admin/api/keys/99999/reveal", nil)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// --- Full Upstream Lifecycle ---

func TestUpstreamLifecycle(t *testing.T) {
	_, router := setupTestAdmin(t)

	// 1. Create
	rr := doRequest(t, router, "POST", "/admin/api/upstreams", map[string]interface{}{
		"name":     "lifecycle-upstream",
		"base_url": "https://api.openai.com",
		"api_keys": []string{"sk-lifecycle"},
		"priority": 10,
		"remark":   "test remark",
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	id := created["id"]
	assert.Equal(t, float64(10), created["priority"])

	// 2. List and verify
	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	require.Len(t, list, 1)
	assert.Equal(t, "lifecycle-upstream", list[0]["name"])
	assert.Equal(t, "test remark", list[0]["remark"])
	assert.Equal(t, true, list[0]["enabled"]) // default enabled

	// 3. Update name + disable
	path := fmt.Sprintf("/admin/api/upstreams/%v", id)
	rr = doRequest(t, router, "PUT", path, map[string]interface{}{
		"name":    "renamed-upstream",
		"enabled": false,
	})
	require.Equal(t, http.StatusOK, rr.Code)
	updated := decodeJSON(t, rr)
	assert.Equal(t, "renamed-upstream", updated["name"])
	assert.Equal(t, false, updated["enabled"])

	// 4. Delete
	rr = doRequest(t, router, "DELETE", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	// 5. Verify gone
	rr = doRequest(t, router, "GET", "/admin/api/upstreams", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	list = decodeJSONArray(t, rr)
	assert.Empty(t, list)
}

// --- Full Key Lifecycle ---

func TestKeyLifecycle(t *testing.T) {
	_, router := setupTestAdmin(t)

	// 1. Create
	rr := doRequest(t, router, "POST", "/admin/api/keys", map[string]interface{}{
		"name":      "lifecycle-key",
		"rpm_limit": 60,
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	created := decodeJSON(t, rr)
	id := created["id"]
	plaintext := created["key"].(string)
	assert.True(t, len(plaintext) > 10, "plaintext key should be reasonably long")
	assert.Equal(t, "lifecycle-key", created["name"])
	assert.Equal(t, float64(60), created["rpm_limit"])

	// 2. List — no plaintext
	rr = doRequest(t, router, "GET", "/admin/api/keys", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	list := decodeJSONArray(t, rr)
	require.Len(t, list, 1)
	assert.Equal(t, "lifecycle-key", list[0]["name"])
	_, hasPlaintext := list[0]["key"]
	assert.False(t, hasPlaintext)

	// 3. Update
	path := fmt.Sprintf("/admin/api/keys/%v", id)
	rr = doRequest(t, router, "PUT", path, map[string]interface{}{
		"name":      "updated-key",
		"rpm_limit": 120,
	})
	require.Equal(t, http.StatusOK, rr.Code)
	updated := decodeJSON(t, rr)
	assert.Equal(t, "updated-key", updated["name"])
	assert.Equal(t, float64(120), updated["rpm_limit"])

	// 4. Delete
	rr = doRequest(t, router, "DELETE", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	// 5. Verify gone
	rr = doRequest(t, router, "GET", "/admin/api/keys", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	list = decodeJSONArray(t, rr)
	assert.Empty(t, list)
}
