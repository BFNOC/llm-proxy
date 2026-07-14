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

// setupTestAdminWithStore is like setupTestAdmin but also returns the store
// so tests can pre-populate data directly.
func setupTestAdminWithStore(t *testing.T) (*AdminHandler, *mux.Router, *store.Store) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	encKey := []byte("01234567890123456789012345678901") // 32 bytes
	s, err := store.NewStore(dbPath, encKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	kc := middleware.NewKeyCache()
	require.NoError(t, kc.Reload(s))

	rl := middleware.NewPerKeyRPMLimiter()
	dp := proxy.NewDynamicProxy()
	prober := proxy.NewUpstreamProber(s, dp, 1*time.Hour, 5*time.Second)
	al := middleware.NewAuditLogger(s, nil, 100, 10, 1*time.Second)
	mf := middleware.NewModelFilter(s)
	rc := middleware.NewGlobalRequestCounter()
	pks := middleware.NewPerKeyStatsCollector()
	oc := middleware.NewModelOverrideCache(s)
	bc := middleware.NewBindingCache(s)
	hc := middleware.NewHeaderCapture(10)

	h := NewAdminHandler(s, kc, rl, prober, dp, al, mf, rc, pks, oc, bc, hc, nil, testAdminToken, "test")
	r := mux.NewRouter()
	h.RegisterRoutes(r)

	return h, r, s
}

// featDoReq builds an authenticated JSON request, fires it, and returns the recorder.
func featDoReq(t *testing.T, router *mux.Router, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// featDecodeMap decodes the response body into a map.
func featDecodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result), "body: %s", rec.Body.String())
	return result
}

// featDecodeArray decodes the response body into a slice of maps.
func featDecodeArray(t *testing.T, rec *httptest.ResponseRecorder) []map[string]interface{} {
	t.Helper()
	var result []map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result), "body: %s", rec.Body.String())
	return result
}

// featDecodeSlice decodes the response body into []interface{}.
func featDecodeSlice(t *testing.T, rec *httptest.ResponseRecorder) []interface{} {
	t.Helper()
	var result []interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result), "body: %s", rec.Body.String())
	return result
}

// seedUpstream creates an upstream via the store and returns its ID.
func seedUpstream(t *testing.T, s *store.Store, name string) int64 {
	t.Helper()
	u, err := s.CreateUpstream(name, "https://api.example.com", []string{"sk-test-key"}, 10, "", "round-robin", "api_key", "", false, false, 0)
	require.NoError(t, err)
	return u.ID
}

// seedKey creates a downstream key via the store and returns its ID.
func seedKey(t *testing.T, s *store.Store, name string) int64 {
	t.Helper()
	_, k, err := s.CreateKey(name, 60)
	require.NoError(t, err)
	return k.ID
}

// ---------------------------------------------------------------------------
// Auth guard (feature endpoints)
// ---------------------------------------------------------------------------

func TestFeatureEndpoints_RequireAuth(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	paths := []struct {
		method string
		path   string
	}{
		{"GET", "/admin/api/keys/bindings"},
		{"GET", "/admin/api/keys/model-overrides"},
		{"GET", "/admin/api/models/whitelist"},
		{"GET", "/admin/api/settings"},
		{"GET", "/admin/api/status"},
	}
	for _, p := range paths {
		t.Run(p.method+" "+p.path, func(t *testing.T) {
			req := httptest.NewRequest(p.method, p.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}

// ---------------------------------------------------------------------------
// Key Upstream Bindings: GET/PUT /admin/api/keys/{id}/upstreams
// ---------------------------------------------------------------------------

func TestGetKeyUpstreams_Empty(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	keyID := seedKey(t, s, "binding-test-key")

	rec := featDoReq(t, router, "GET", fmt.Sprintf("/admin/api/keys/%d/upstreams", keyID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := featDecodeMap(t, rec)
	ids, ok := resp["upstream_ids"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, ids)
}

func TestGetKeyUpstreams_NotFound(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	rec := featDoReq(t, router, "GET", "/admin/api/keys/99999/upstreams", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSetAndGetKeyUpstreams(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	keyID := seedKey(t, s, "bind-key")
	u1 := seedUpstream(t, s, "upstream-a")
	u2 := seedUpstream(t, s, "upstream-b")

	// PUT bindings
	rec := featDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/keys/%d/upstreams", keyID), map[string]interface{}{
		"upstream_ids": []int64{u1, u2},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	putResp := featDecodeMap(t, rec)
	assert.Equal(t, "updated", putResp["status"])

	// GET to verify
	rec = featDoReq(t, router, "GET", fmt.Sprintf("/admin/api/keys/%d/upstreams", keyID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	getResp := featDecodeMap(t, rec)
	ids := getResp["upstream_ids"].([]interface{})
	assert.Len(t, ids, 2)
}

func TestSetKeyUpstreams_ClearBindings(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	keyID := seedKey(t, s, "clear-bind-key")
	u1 := seedUpstream(t, s, "upstream-clear")

	// Set then clear
	featDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/keys/%d/upstreams", keyID), map[string]interface{}{
		"upstream_ids": []int64{u1},
	})
	rec := featDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/keys/%d/upstreams", keyID), map[string]interface{}{
		"upstream_ids": []int64{},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify empty
	rec = featDoReq(t, router, "GET", fmt.Sprintf("/admin/api/keys/%d/upstreams", keyID), nil)
	resp := featDecodeMap(t, rec)
	assert.Empty(t, resp["upstream_ids"].([]interface{}))
}

func TestSetKeyUpstreams_InvalidUpstream(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	keyID := seedKey(t, s, "bad-bind")

	rec := featDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/keys/%d/upstreams", keyID), map[string]interface{}{
		"upstream_ids": []int64{99999},
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSetKeyUpstreams_DeduplicatesIDs(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	keyID := seedKey(t, s, "dedup-bind")
	u1 := seedUpstream(t, s, "upstream-dedup")

	rec := featDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/keys/%d/upstreams", keyID), map[string]interface{}{
		"upstream_ids": []int64{u1, u1, u1},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify only one binding
	rec = featDoReq(t, router, "GET", fmt.Sprintf("/admin/api/keys/%d/upstreams", keyID), nil)
	resp := featDecodeMap(t, rec)
	assert.Len(t, resp["upstream_ids"].([]interface{}), 1)
}

// ---------------------------------------------------------------------------
// All Key Bindings Overview: GET /admin/api/keys/bindings
// ---------------------------------------------------------------------------

func TestGetAllKeyBindings(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	k1 := seedKey(t, s, "overview-k1")
	u1 := seedUpstream(t, s, "overview-u1")

	require.NoError(t, s.SetKeyUpstreams(k1, []int64{u1}))

	rec := featDoReq(t, router, "GET", "/admin/api/keys/bindings", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := featDecodeMap(t, rec)
	// k1 should appear (keyed by string form of ID)
	assert.Contains(t, resp, fmt.Sprintf("%d", k1))
}

func TestGetAllKeyBindings_Empty(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	rec := featDoReq(t, router, "GET", "/admin/api/keys/bindings", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	// Should be valid JSON (empty map)
	resp := featDecodeMap(t, rec)
	assert.NotNil(t, resp)
}

// ---------------------------------------------------------------------------
// Key Model Overrides: GET/PUT /admin/api/keys/{id}/model-overrides
// ---------------------------------------------------------------------------

func TestGetKeyModelOverrides_Empty(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	keyID := seedKey(t, s, "override-empty")

	rec := featDoReq(t, router, "GET", fmt.Sprintf("/admin/api/keys/%d/model-overrides", keyID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	overrides := featDecodeSlice(t, rec)
	assert.Empty(t, overrides)
}

func TestGetKeyModelOverrides_NotFound(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	rec := featDoReq(t, router, "GET", "/admin/api/keys/99999/model-overrides", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSetAndGetKeyModelOverrides(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	keyID := seedKey(t, s, "override-key")
	u1 := seedUpstream(t, s, "override-u1")

	// PUT overrides
	rec := featDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/keys/%d/model-overrides", keyID), map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"model_pattern": "gpt-*", "upstream_id": u1},
			{"model_pattern": "claude-*", "upstream_id": u1},
		},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	putResp := featDecodeMap(t, rec)
	assert.Equal(t, "updated", putResp["status"])
	assert.Equal(t, float64(2), putResp["count"])

	// GET to verify
	rec = featDoReq(t, router, "GET", fmt.Sprintf("/admin/api/keys/%d/model-overrides", keyID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	overrides := featDecodeArray(t, rec)
	assert.Len(t, overrides, 2)
}

func TestSetKeyModelOverrides_ClearOverrides(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	keyID := seedKey(t, s, "clear-override")
	u1 := seedUpstream(t, s, "clear-override-u")

	// Set then clear
	featDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/keys/%d/model-overrides", keyID), map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"model_pattern": "gpt-*", "upstream_id": u1},
		},
	})
	rec := featDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/keys/%d/model-overrides", keyID), map[string]interface{}{
		"overrides": []map[string]interface{}{},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify empty
	rec = featDoReq(t, router, "GET", fmt.Sprintf("/admin/api/keys/%d/model-overrides", keyID), nil)
	overrides := featDecodeSlice(t, rec)
	assert.Empty(t, overrides)
}

func TestSetKeyModelOverrides_ValidationErrors(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	keyID := seedKey(t, s, "validate-override")
	u1 := seedUpstream(t, s, "validate-override-u")

	tests := []struct {
		name string
		body map[string]interface{}
	}{
		{
			name: "invalid glob pattern",
			body: map[string]interface{}{
				"overrides": []map[string]interface{}{
					{"model_pattern": "[invalid", "upstream_id": u1},
				},
			},
		},
		{
			name: "nonexistent upstream",
			body: map[string]interface{}{
				"overrides": []map[string]interface{}{
					{"model_pattern": "gpt-*", "upstream_id": 99999},
				},
			},
		},
		{
			name: "duplicate override",
			body: map[string]interface{}{
				"overrides": []map[string]interface{}{
					{"model_pattern": "gpt-*", "upstream_id": u1},
					{"model_pattern": "gpt-*", "upstream_id": u1},
				},
			},
		},
		{
			name: "empty model_pattern",
			body: map[string]interface{}{
				"overrides": []map[string]interface{}{
					{"model_pattern": "", "upstream_id": u1},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := featDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/keys/%d/model-overrides", keyID), tc.body)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestSetKeyModelOverrides_KeyNotFound(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	rec := featDoReq(t, router, "PUT", "/admin/api/keys/99999/model-overrides", map[string]interface{}{
		"overrides": []map[string]interface{}{},
	})
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ---------------------------------------------------------------------------
// All Key Model Overrides Overview: GET /admin/api/keys/model-overrides
// ---------------------------------------------------------------------------

func TestGetAllKeyModelOverrides(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	k1 := seedKey(t, s, "all-overrides-k1")
	u1 := seedUpstream(t, s, "all-overrides-u1")

	require.NoError(t, s.SetKeyModelOverrides(k1, []store.KeyModelOverrideInput{
		{ModelPattern: "gpt-*", UpstreamID: u1},
	}))

	rec := featDoReq(t, router, "GET", "/admin/api/keys/model-overrides", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := featDecodeMap(t, rec)
	assert.NotEmpty(t, resp)
}

func TestGetAllKeyModelOverrides_Empty(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	rec := featDoReq(t, router, "GET", "/admin/api/keys/model-overrides", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Should return a valid map (possibly empty), never null
	resp := featDecodeMap(t, rec)
	assert.NotNil(t, resp)
}

// ---------------------------------------------------------------------------
// Model Whitelist: GET/POST /admin/api/models/whitelist
//                  DELETE /admin/api/models/whitelist/{id}
//                  DELETE /admin/api/models/whitelist/batch
// ---------------------------------------------------------------------------

func TestListModelWhitelist_Empty(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	rec := featDoReq(t, router, "GET", "/admin/api/models/whitelist", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	entries := featDecodeSlice(t, rec)
	assert.Empty(t, entries)
}

func TestAddAndListModelWhitelist(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	// Add pattern
	rec := featDoReq(t, router, "POST", "/admin/api/models/whitelist", map[string]string{
		"pattern": "gpt-*",
	})
	assert.Equal(t, http.StatusCreated, rec.Code)

	entry := featDecodeMap(t, rec)
	assert.Equal(t, "gpt-*", entry["Pattern"])
	assert.NotNil(t, entry["ID"])

	// List to verify
	rec = featDoReq(t, router, "GET", "/admin/api/models/whitelist", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	entries := featDecodeArray(t, rec)
	assert.Len(t, entries, 1)
	assert.Equal(t, "gpt-*", entries[0]["Pattern"])
}

func TestAddModelWhitelist_EmptyPattern(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	rec := featDoReq(t, router, "POST", "/admin/api/models/whitelist", map[string]string{
		"pattern": "",
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAddModelWhitelist_InvalidGlob(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	rec := featDoReq(t, router, "POST", "/admin/api/models/whitelist", map[string]string{
		"pattern": "[invalid",
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAddModelWhitelist_ValidPatterns(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	patterns := []string{
		"gpt-*",
		"claude-3.5-*",
		"llama-3-*-instruct",
		"*",
	}
	for _, p := range patterns {
		t.Run(p, func(t *testing.T) {
			rec := featDoReq(t, router, "POST", "/admin/api/models/whitelist", map[string]string{"pattern": p})
			assert.Equal(t, http.StatusCreated, rec.Code, "pattern %q should be accepted", p)
		})
	}
}

func TestDeleteModelWhitelist(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)

	entry, err := s.AddModelWhitelist("claude-*")
	require.NoError(t, err)

	rec := featDoReq(t, router, "DELETE", fmt.Sprintf("/admin/api/models/whitelist/%d", entry.ID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify deleted
	rec = featDoReq(t, router, "GET", "/admin/api/models/whitelist", nil)
	entries := featDecodeSlice(t, rec)
	assert.Empty(t, entries)
}

func TestBatchDeleteModelWhitelist(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)

	e1, err := s.AddModelWhitelist("gpt-*")
	require.NoError(t, err)
	e2, err := s.AddModelWhitelist("claude-*")
	require.NoError(t, err)
	_, err = s.AddModelWhitelist("llama-*")
	require.NoError(t, err)

	// Delete first two
	rec := featDoReq(t, router, "DELETE", "/admin/api/models/whitelist/batch", map[string]interface{}{
		"ids": []int64{e1.ID, e2.ID},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := featDecodeMap(t, rec)
	assert.Equal(t, "deleted", resp["status"])
	assert.Equal(t, float64(2), resp["deleted"])

	// Verify only llama-* remains
	rec = featDoReq(t, router, "GET", "/admin/api/models/whitelist", nil)
	entries := featDecodeArray(t, rec)
	assert.Len(t, entries, 1)
	assert.Equal(t, "llama-*", entries[0]["Pattern"])
}

func TestBatchDeleteModelWhitelist_EmptyIDs(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	rec := featDoReq(t, router, "DELETE", "/admin/api/models/whitelist/batch", map[string]interface{}{
		"ids": []int64{},
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestModelWhitelist_AddMultipleAndDeleteOne(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	// Add two patterns
	rec1 := featDoReq(t, router, "POST", "/admin/api/models/whitelist", map[string]string{"pattern": "gpt-4*"})
	assert.Equal(t, http.StatusCreated, rec1.Code)
	e1 := featDecodeMap(t, rec1)

	rec2 := featDoReq(t, router, "POST", "/admin/api/models/whitelist", map[string]string{"pattern": "claude-3*"})
	assert.Equal(t, http.StatusCreated, rec2.Code)

	// Delete first
	rec := featDoReq(t, router, "DELETE", fmt.Sprintf("/admin/api/models/whitelist/%.0f", e1["ID"].(float64)), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify only claude-3* remains
	rec = featDoReq(t, router, "GET", "/admin/api/models/whitelist", nil)
	entries := featDecodeArray(t, rec)
	assert.Len(t, entries, 1)
	assert.Equal(t, "claude-3*", entries[0]["Pattern"])
}

// ---------------------------------------------------------------------------
// Settings: GET/PUT /admin/api/settings
// ---------------------------------------------------------------------------

func TestGetSettings_Default(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	rec := featDoReq(t, router, "GET", "/admin/api/settings", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := featDecodeMap(t, rec)
	assert.Contains(t, resp, "auto_disable_threshold")
	// Default threshold is 0 (from atomic default)
	assert.Equal(t, float64(0), resp["auto_disable_threshold"])
}

func TestUpdateAndGetSettings(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	// Update threshold
	rec := featDoReq(t, router, "PUT", "/admin/api/settings", map[string]interface{}{
		"auto_disable_threshold": 5,
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	putResp := featDecodeMap(t, rec)
	assert.Equal(t, "updated", putResp["status"])

	// GET to verify
	rec = featDoReq(t, router, "GET", "/admin/api/settings", nil)
	getResp := featDecodeMap(t, rec)
	assert.Equal(t, float64(5), getResp["auto_disable_threshold"])
}

func TestUpdateSettings_NegativeThreshold(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	rec := featDoReq(t, router, "PUT", "/admin/api/settings", map[string]interface{}{
		"auto_disable_threshold": -1,
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUpdateSettings_ZeroThreshold(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	// Set to non-zero first
	featDoReq(t, router, "PUT", "/admin/api/settings", map[string]interface{}{
		"auto_disable_threshold": 10,
	})
	// Set back to zero (disables feature)
	rec := featDoReq(t, router, "PUT", "/admin/api/settings", map[string]interface{}{
		"auto_disable_threshold": 0,
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = featDoReq(t, router, "GET", "/admin/api/settings", nil)
	resp := featDecodeMap(t, rec)
	assert.Equal(t, float64(0), resp["auto_disable_threshold"])
}

func TestUpdateSettings_NoThresholdField(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	// Empty JSON body (no threshold) succeeds without changing anything
	rec := featDoReq(t, router, "PUT", "/admin/api/settings", map[string]interface{}{})
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestUpdateSettings_InvalidJSON(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	req := httptest.NewRequest("PUT", "/admin/api/settings", bytes.NewBufferString("not json"))
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// Status: GET /admin/api/status
// ---------------------------------------------------------------------------

func TestGetStatus_ResponseStructure(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	rec := featDoReq(t, router, "GET", "/admin/api/status", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := featDecodeMap(t, rec)

	requiredFields := []string{
		"healthy_upstreams",
		"total_keys",
		"today_requests",
		"audit_dropped",
		"uptime",
		"version",
		"timestamp",
		"active_requests",
		"rpm",
		"rps",
		"transport_pool",
	}
	for _, f := range requiredFields {
		assert.Contains(t, resp, f, "missing field: %s", f)
	}
	assert.Equal(t, "test", resp["version"])
}

func TestGetStatus_HealthyUpstreamsIsArray(t *testing.T) {
	_, router, _ := setupTestAdminWithStore(t)

	rec := featDoReq(t, router, "GET", "/admin/api/status", nil)
	resp := featDecodeMap(t, rec)

	// Should be an array (possibly empty), never null
	upstreams, ok := resp["healthy_upstreams"].([]interface{})
	assert.True(t, ok, "healthy_upstreams should be an array")
	assert.NotNil(t, upstreams)
}

func TestGetStatus_WithKeys(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	seedKey(t, s, "status-key-1")
	seedKey(t, s, "status-key-2")

	rec := featDoReq(t, router, "GET", "/admin/api/status", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := featDecodeMap(t, rec)
	assert.Equal(t, float64(2), resp["total_keys"])
}

// ---------------------------------------------------------------------------
// Upstream API Keys: GET/POST/DELETE /admin/api/upstreams/{id}/apikeys
//                    PUT /admin/api/upstreams/{id}/apikeys/{key_id}/enabled
// ---------------------------------------------------------------------------

func TestListUpstreamAPIKeys_Empty(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	u, err := s.CreateUpstream("no-keys", "https://api.example.com", []string{}, 10, "", "round-robin", "api_key", "", false, false, 0)
	require.NoError(t, err)

	rec := featDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/apikeys", u.ID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	entries := featDecodeSlice(t, rec)
	assert.Empty(t, entries)
}

func TestListUpstreamAPIKeys_WithKeys(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	uID := seedUpstream(t, s, "list-keys-upstream")

	rec := featDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/apikeys", uID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	entries := featDecodeArray(t, rec)
	assert.Len(t, entries, 1) // seedUpstream adds one key
	assert.Contains(t, entries[0], "row_id")
	assert.Contains(t, entries[0], "key")
	assert.Contains(t, entries[0], "enabled")
	assert.Equal(t, true, entries[0]["enabled"])
}

func TestAddUpstreamAPIKeys(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	uID := seedUpstream(t, s, "add-keys-upstream")

	rec := featDoReq(t, router, "POST", fmt.Sprintf("/admin/api/upstreams/%d/apikeys", uID), map[string]interface{}{
		"api_keys": []string{"sk-new-key-1", "sk-new-key-2"},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := featDecodeMap(t, rec)
	assert.Equal(t, "created", resp["status"])
	assert.Equal(t, float64(2), resp["count"])

	// Verify total keys (original 1 + 2 new)
	rec = featDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/apikeys", uID), nil)
	entries := featDecodeSlice(t, rec)
	assert.Len(t, entries, 3)
}

func TestAddUpstreamAPIKeys_SingleKeyCompat(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	uID := seedUpstream(t, s, "single-key-upstream")

	rec := featDoReq(t, router, "POST", fmt.Sprintf("/admin/api/upstreams/%d/apikeys", uID), map[string]interface{}{
		"api_key": "sk-single-compat",
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := featDecodeMap(t, rec)
	assert.Equal(t, "created", resp["status"])
}

func TestAddUpstreamAPIKeys_Empty(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	uID := seedUpstream(t, s, "empty-keys-upstream")

	rec := featDoReq(t, router, "POST", fmt.Sprintf("/admin/api/upstreams/%d/apikeys", uID), map[string]interface{}{
		"api_keys": []string{},
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAddUpstreamAPIKeys_MultiLineInput(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	uID := seedUpstream(t, s, "multiline-upstream")

	// Simulate multi-line paste (newline-separated keys in a single string)
	rec := featDoReq(t, router, "POST", fmt.Sprintf("/admin/api/upstreams/%d/apikeys", uID), map[string]interface{}{
		"api_key": "sk-key-line1\nsk-key-line2\nsk-key-line3",
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := featDecodeMap(t, rec)
	assert.Equal(t, "created", resp["status"])

	// Verify all 3 new keys added (+ 1 original = 4)
	rec = featDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/apikeys", uID), nil)
	entries := featDecodeSlice(t, rec)
	assert.Len(t, entries, 4)
}

func TestDeleteUpstreamAPIKey(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	uID := seedUpstream(t, s, "del-key-upstream")

	keys, err := s.GetUpstreamAllAPIKeys(uID)
	require.NoError(t, err)
	require.Len(t, keys, 1)

	rec := featDoReq(t, router, "DELETE", fmt.Sprintf("/admin/api/upstreams/%d/apikeys/%d", uID, keys[0].RowID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := featDecodeMap(t, rec)
	assert.Equal(t, "deleted", resp["status"])

	// Verify deleted
	rec = featDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/apikeys", uID), nil)
	remaining := featDecodeSlice(t, rec)
	assert.Empty(t, remaining)
}

func TestDeleteUpstreamAPIKey_NotFound(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	uID := seedUpstream(t, s, "del-notfound-upstream")

	rec := featDoReq(t, router, "DELETE", fmt.Sprintf("/admin/api/upstreams/%d/apikeys/99999", uID), nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSetAPIKeyEnabled(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	uID := seedUpstream(t, s, "toggle-key-upstream")

	keys, err := s.GetUpstreamAllAPIKeys(uID)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	keyRowID := keys[0].RowID

	tests := []struct {
		name    string
		enabled bool
	}{
		{"disable", false},
		{"enable", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := featDoReq(t, router, "PUT",
				fmt.Sprintf("/admin/api/upstreams/%d/apikeys/%d/enabled", uID, keyRowID),
				map[string]interface{}{"enabled": tc.enabled})
			assert.Equal(t, http.StatusOK, rec.Code)

			resp := featDecodeMap(t, rec)
			assert.Equal(t, tc.enabled, resp["enabled"])

			// Verify via list
			rec = featDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/apikeys", uID), nil)
			apiKeys := featDecodeArray(t, rec)
			require.Len(t, apiKeys, 1)
			assert.Equal(t, tc.enabled, apiKeys[0]["enabled"])
		})
	}
}

func TestSetAPIKeyEnabled_NotFound(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	uID := seedUpstream(t, s, "toggle-notfound")

	rec := featDoReq(t, router, "PUT",
		fmt.Sprintf("/admin/api/upstreams/%d/apikeys/99999/enabled", uID),
		map[string]interface{}{"enabled": false})
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ---------------------------------------------------------------------------
// Integration: bindings + overrides full lifecycle
// ---------------------------------------------------------------------------

func TestBindingsAndOverrides_FullLifecycle(t *testing.T) {
	_, router, s := setupTestAdminWithStore(t)
	keyID := seedKey(t, s, "lifecycle-key")
	u1 := seedUpstream(t, s, "lifecycle-u1")
	u2 := seedUpstream(t, s, "lifecycle-u2")

	// 1: Set bindings
	rec := featDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/keys/%d/upstreams", keyID), map[string]interface{}{
		"upstream_ids": []int64{u1, u2},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	// 2: Set model overrides
	rec = featDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/keys/%d/model-overrides", keyID), map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"model_pattern": "gpt-*", "upstream_id": u1},
			{"model_pattern": "claude-*", "upstream_id": u2},
		},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	// 3: Verify all bindings overview
	rec = featDoReq(t, router, "GET", "/admin/api/keys/bindings", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	bindingsOverview := featDecodeMap(t, rec)
	assert.Contains(t, bindingsOverview, fmt.Sprintf("%d", keyID))

	// 4: Verify all overrides overview
	rec = featDoReq(t, router, "GET", "/admin/api/keys/model-overrides", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	overridesOverview := featDecodeMap(t, rec)
	assert.NotEmpty(t, overridesOverview)

	// 5: Clear bindings
	rec = featDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/keys/%d/upstreams", keyID), map[string]interface{}{
		"upstream_ids": []int64{},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	// 6: Clear overrides
	rec = featDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/keys/%d/model-overrides", keyID), map[string]interface{}{
		"overrides": []map[string]interface{}{},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	// Final: verify all empty
	rec = featDoReq(t, router, "GET", fmt.Sprintf("/admin/api/keys/%d/upstreams", keyID), nil)
	bindResp := featDecodeMap(t, rec)
	assert.Empty(t, bindResp["upstream_ids"].([]interface{}))

	rec = featDoReq(t, router, "GET", fmt.Sprintf("/admin/api/keys/%d/model-overrides", keyID), nil)
	overResp := featDecodeSlice(t, rec)
	assert.Empty(t, overResp)
}
