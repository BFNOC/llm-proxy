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

// setupExtraTestAdmin creates an isolated AdminHandler with its own Store for
// tests in this file. Returns the handler, router, and store so tests can
// pre-populate data directly.
func setupExtraTestAdmin(t *testing.T) (*AdminHandler, *mux.Router, *store.Store) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "extra_test.db")
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

// extraDoReq builds an authenticated JSON request, fires it, and returns the recorder.
func extraDoReq(t *testing.T, router *mux.Router, method, path string, body interface{}) *httptest.ResponseRecorder {
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

// extraDecodeMap decodes the response body into a map.
func extraDecodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result), "body: %s", rec.Body.String())
	return result
}

// extraDecodeArray decodes the response body into a slice of maps.
func extraDecodeArray(t *testing.T, rec *httptest.ResponseRecorder) []map[string]interface{} {
	t.Helper()
	var result []map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result), "body: %s", rec.Body.String())
	return result
}

// insertTestLogs inserts audit log entries via the store for testing.
func insertTestLogs(t *testing.T, s *store.Store, keyID int64, count int, baseTime time.Time, statusCode int) {
	t.Helper()
	logs := make([]store.RequestLog, count)
	for i := 0; i < count; i++ {
		logs[i] = store.RequestLog{
			DownstreamKeyID: keyID,
			UpstreamName:    "test-upstream",
			UpstreamKeyIdx:  0,
			Model:           "gpt-4o",
			UsedProxy:       "",
			ClientIP:        "127.0.0.1",
			IPRegion:        "CN",
			ProviderStyle:   "openai",
			Path:            "/v1/chat/completions",
			StatusCode:      statusCode,
			LatencyMs:       int64(50 + i*10),
			CreatedAt:       baseTime.Add(time.Duration(i) * time.Minute),
		}
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))
}

// ---------------------------------------------------------------------------
// Logs: GET /admin/api/logs
// ---------------------------------------------------------------------------

func TestQueryLogs_NoParams(t *testing.T) {
	_, router, s := setupExtraTestAdmin(t)
	_, dk, err := s.CreateKey("log-key", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	insertTestLogs(t, s, dk.ID, 3, now.Add(-10*time.Minute), 200)

	rec := extraDoReq(t, router, "GET", "/admin/api/logs", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	logs := extraDecodeArray(t, rec)
	assert.Len(t, logs, 3)
}

func TestQueryLogs_Empty(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	rec := extraDoReq(t, router, "GET", "/admin/api/logs", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	var logs []interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &logs))
	assert.Empty(t, logs)
}

func TestQueryLogs_FilterByKeyID(t *testing.T) {
	_, router, s := setupExtraTestAdmin(t)
	_, dk1, err := s.CreateKey("key-a", 0)
	require.NoError(t, err)
	_, dk2, err := s.CreateKey("key-b", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	insertTestLogs(t, s, dk1.ID, 2, now.Add(-10*time.Minute), 200)
	insertTestLogs(t, s, dk2.ID, 3, now.Add(-10*time.Minute), 200)

	rec := extraDoReq(t, router, "GET", fmt.Sprintf("/admin/api/logs?key_id=%d", dk1.ID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	logs := extraDecodeArray(t, rec)
	assert.Len(t, logs, 2)
	for _, l := range logs {
		assert.Equal(t, float64(dk1.ID), l["DownstreamKeyID"])
	}
}

func TestQueryLogs_FilterByDateRange(t *testing.T) {
	_, router, s := setupExtraTestAdmin(t)
	_, dk, err := s.CreateKey("date-key", 0)
	require.NoError(t, err)

	// Insert logs at known times
	baseTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	insertTestLogs(t, s, dk.ID, 5, baseTime, 200)

	// Query a narrow window that only includes the first 2 logs (minutes 0 and 1)
	from := baseTime.Add(-1 * time.Second).Format(time.RFC3339)
	to := baseTime.Add(1*time.Minute + 30*time.Second).Format(time.RFC3339)

	rec := extraDoReq(t, router, "GET", fmt.Sprintf("/admin/api/logs?from=%s&to=%s", from, to), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	logs := extraDecodeArray(t, rec)
	assert.Len(t, logs, 2)
}

func TestQueryLogs_WithLimit(t *testing.T) {
	_, router, s := setupExtraTestAdmin(t)
	_, dk, err := s.CreateKey("limit-key", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	insertTestLogs(t, s, dk.ID, 10, now.Add(-20*time.Minute), 200)

	rec := extraDoReq(t, router, "GET", "/admin/api/logs?limit=3", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	logs := extraDecodeArray(t, rec)
	assert.Len(t, logs, 3)
}

func TestQueryLogs_InvalidParams(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	tests := []struct {
		name  string
		query string
	}{
		{"invalid key_id", "/admin/api/logs?key_id=abc"},
		{"invalid from date", "/admin/api/logs?from=not-a-date"},
		{"invalid to date", "/admin/api/logs?to=not-a-date"},
		{"invalid limit", "/admin/api/logs?limit=xyz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := extraDoReq(t, router, "GET", tt.query, nil)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestQueryLogs_ResponseFields(t *testing.T) {
	_, router, s := setupExtraTestAdmin(t)
	_, dk, err := s.CreateKey("fields-key", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	insertTestLogs(t, s, dk.ID, 1, now.Add(-5*time.Minute), 200)

	rec := extraDoReq(t, router, "GET", "/admin/api/logs", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	logs := extraDecodeArray(t, rec)
	require.Len(t, logs, 1)
	entry := logs[0]

	// Verify essential fields are present
	expectedFields := []string{
		"ID", "DownstreamKeyID", "UpstreamName", "Model",
		"Path", "StatusCode", "LatencyMs", "CreatedAt",
	}
	for _, f := range expectedFields {
		assert.Contains(t, entry, f, "missing field: %s", f)
	}
}

// ---------------------------------------------------------------------------
// Key Usage Stats: GET /admin/api/logs/key-stats
// ---------------------------------------------------------------------------

func TestGetKeyUsageStats_Empty(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	rec := extraDoReq(t, router, "GET", "/admin/api/logs/key-stats", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	var stats []interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &stats))
	assert.Empty(t, stats)
}

func TestGetKeyUsageStats_WithLogs(t *testing.T) {
	_, router, s := setupExtraTestAdmin(t)
	_, dk, err := s.CreateKey("stats-key", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	// Insert success logs
	insertTestLogs(t, s, dk.ID, 3, now.Add(-10*time.Minute), 200)
	// Insert error logs
	insertTestLogs(t, s, dk.ID, 2, now.Add(-5*time.Minute), 500)

	rec := extraDoReq(t, router, "GET", "/admin/api/logs/key-stats", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	stats := extraDecodeArray(t, rec)
	require.Len(t, stats, 1, "should have stats for one key")

	s0 := stats[0]
	assert.Equal(t, float64(dk.ID), s0["key_id"])
	assert.Equal(t, float64(5), s0["total"])
	assert.Equal(t, float64(3), s0["success"])
	assert.Equal(t, float64(2), s0["error"])
	assert.Contains(t, s0, "avg_latency_ms")
}

func TestGetKeyUsageStats_MultipleKeys(t *testing.T) {
	_, router, s := setupExtraTestAdmin(t)
	_, dk1, err := s.CreateKey("stats-key-1", 0)
	require.NoError(t, err)
	_, dk2, err := s.CreateKey("stats-key-2", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	insertTestLogs(t, s, dk1.ID, 4, now.Add(-10*time.Minute), 200)
	insertTestLogs(t, s, dk2.ID, 2, now.Add(-5*time.Minute), 403)

	rec := extraDoReq(t, router, "GET", "/admin/api/logs/key-stats", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	stats := extraDecodeArray(t, rec)
	assert.Len(t, stats, 2, "should have stats for two keys")

	// Build a lookup by key_id for easier assertions
	byKey := make(map[float64]map[string]interface{})
	for _, s := range stats {
		byKey[s["key_id"].(float64)] = s
	}

	s1 := byKey[float64(dk1.ID)]
	require.NotNil(t, s1)
	assert.Equal(t, float64(4), s1["total"])
	assert.Equal(t, float64(4), s1["success"])
	assert.Equal(t, float64(0), s1["error"])

	s2 := byKey[float64(dk2.ID)]
	require.NotNil(t, s2)
	assert.Equal(t, float64(2), s2["total"])
	assert.Equal(t, float64(0), s2["success"])
	assert.Equal(t, float64(2), s2["error"])
}

// ---------------------------------------------------------------------------
// Test Models CRUD: /admin/api/test-models
// ---------------------------------------------------------------------------

func TestListTestModels_Empty(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	rec := extraDoReq(t, router, "GET", "/admin/api/test-models", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	var models []interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &models))
	assert.Empty(t, models)
}

func TestCreateTestModel(t *testing.T) {
	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
		wantError  bool
	}{
		{
			name:       "valid create with protocol",
			body:       map[string]interface{}{"name": "gpt-4o", "protocol": "openai"},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "valid create defaults to openai protocol",
			body:       map[string]interface{}{"name": "claude-3.5-sonnet"},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "anthropic protocol",
			body:       map[string]interface{}{"name": "claude-opus", "protocol": "anthropic"},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "missing name returns 400",
			body:       map[string]interface{}{"protocol": "openai"},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, router, _ := setupExtraTestAdmin(t)
			rec := extraDoReq(t, router, "POST", "/admin/api/test-models", tt.body)
			assert.Equal(t, tt.wantStatus, rec.Code)
			if !tt.wantError {
				result := extraDecodeMap(t, rec)
				assert.NotNil(t, result["id"])
				assert.Equal(t, tt.body["name"], result["name"])
			}
		})
	}
}

func TestCreateTestModel_ThenList(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	// Create two models
	for _, name := range []string{"gpt-4o", "claude-3.5-sonnet"} {
		rec := extraDoReq(t, router, "POST", "/admin/api/test-models", map[string]interface{}{
			"name":     name,
			"protocol": "openai",
		})
		require.Equal(t, http.StatusCreated, rec.Code)
	}

	// List
	rec := extraDoReq(t, router, "GET", "/admin/api/test-models", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	models := extraDecodeArray(t, rec)
	assert.Len(t, models, 2)
}

func TestListTestModels_FilterByProtocol(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	// Create models with different protocols
	extraDoReq(t, router, "POST", "/admin/api/test-models", map[string]interface{}{
		"name": "gpt-4o", "protocol": "openai",
	})
	extraDoReq(t, router, "POST", "/admin/api/test-models", map[string]interface{}{
		"name": "claude-opus", "protocol": "anthropic",
	})

	// Filter by protocol
	rec := extraDoReq(t, router, "GET", "/admin/api/test-models?protocol=anthropic", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	models := extraDecodeArray(t, rec)
	assert.Len(t, models, 1)
	assert.Equal(t, "claude-opus", models[0]["name"])
	assert.Equal(t, "anthropic", models[0]["protocol"])
}

func TestUpdateTestModel(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	// Create
	rec := extraDoReq(t, router, "POST", "/admin/api/test-models", map[string]interface{}{
		"name":     "original-model",
		"protocol": "openai",
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	created := extraDecodeMap(t, rec)
	id := created["id"]

	// Update
	path := fmt.Sprintf("/admin/api/test-models/%.0f", id.(float64))
	rec = extraDoReq(t, router, "PUT", path, map[string]interface{}{
		"name":     "updated-model",
		"protocol": "anthropic",
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	result := extraDecodeMap(t, rec)
	assert.Equal(t, "updated-model", result["name"])
	assert.Equal(t, "anthropic", result["protocol"])
}

func TestUpdateTestModel_NotFound(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	rec := extraDoReq(t, router, "PUT", "/admin/api/test-models/99999", map[string]interface{}{
		"name":     "nope",
		"protocol": "openai",
	})
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUpdateTestModel_MissingName(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	// Create
	rec := extraDoReq(t, router, "POST", "/admin/api/test-models", map[string]interface{}{
		"name": "to-update", "protocol": "openai",
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	created := extraDecodeMap(t, rec)
	id := created["id"]

	// Update with missing name
	path := fmt.Sprintf("/admin/api/test-models/%.0f", id.(float64))
	rec = extraDoReq(t, router, "PUT", path, map[string]interface{}{
		"protocol": "anthropic",
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDeleteTestModel(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	// Create
	rec := extraDoReq(t, router, "POST", "/admin/api/test-models", map[string]interface{}{
		"name":     "to-delete-model",
		"protocol": "openai",
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	created := extraDecodeMap(t, rec)
	id := created["id"]

	// Delete
	path := fmt.Sprintf("/admin/api/test-models/%.0f", id.(float64))
	rec = extraDoReq(t, router, "DELETE", path, nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	result := extraDecodeMap(t, rec)
	assert.Equal(t, "deleted", result["status"])

	// Verify list is empty
	rec = extraDoReq(t, router, "GET", "/admin/api/test-models", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	var models []interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &models))
	assert.Empty(t, models)
}

func TestDeleteTestModel_NotFound(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	rec := extraDoReq(t, router, "DELETE", "/admin/api/test-models/99999", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestTestModelLifecycle(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	// 1. Create
	rec := extraDoReq(t, router, "POST", "/admin/api/test-models", map[string]interface{}{
		"name":     "lifecycle-model",
		"protocol": "openai",
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	created := extraDecodeMap(t, rec)
	id := created["id"]
	assert.Equal(t, "lifecycle-model", created["name"])
	assert.Equal(t, "openai", created["protocol"])
	assert.NotNil(t, created["created_at"])

	// 2. List
	rec = extraDoReq(t, router, "GET", "/admin/api/test-models", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	models := extraDecodeArray(t, rec)
	require.Len(t, models, 1)
	assert.Equal(t, "lifecycle-model", models[0]["name"])

	// 3. Update
	path := fmt.Sprintf("/admin/api/test-models/%.0f", id.(float64))
	rec = extraDoReq(t, router, "PUT", path, map[string]interface{}{
		"name":     "renamed-model",
		"protocol": "anthropic",
	})
	require.Equal(t, http.StatusOK, rec.Code)
	updated := extraDecodeMap(t, rec)
	assert.Equal(t, "renamed-model", updated["name"])
	assert.Equal(t, "anthropic", updated["protocol"])

	// 4. Delete
	rec = extraDoReq(t, router, "DELETE", path, nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 5. Verify gone
	rec = extraDoReq(t, router, "GET", "/admin/api/test-models", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var empty []interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &empty))
	assert.Empty(t, empty)
}

// ---------------------------------------------------------------------------
// Header Capture: GET/PUT/DELETE /admin/api/header-capture
// ---------------------------------------------------------------------------

func TestGetHeaderCapture_Default(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	rec := extraDoReq(t, router, "GET", "/admin/api/header-capture", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := extraDecodeMap(t, rec)
	assert.Equal(t, false, resp["enabled"])
	assert.Contains(t, resp, "captures")
	assert.Contains(t, resp, "hint")

	captures, ok := resp["captures"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, captures)
}

func TestUpdateHeaderCapture_Enable(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	// Enable
	rec := extraDoReq(t, router, "PUT", "/admin/api/header-capture", map[string]interface{}{
		"enabled": true,
	})
	assert.Equal(t, http.StatusOK, rec.Code)
	resp := extraDecodeMap(t, rec)
	assert.Equal(t, true, resp["enabled"])

	// Verify via GET
	rec = extraDoReq(t, router, "GET", "/admin/api/header-capture", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	resp = extraDecodeMap(t, rec)
	assert.Equal(t, true, resp["enabled"])
}

func TestUpdateHeaderCapture_Disable(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	// Enable first
	extraDoReq(t, router, "PUT", "/admin/api/header-capture", map[string]interface{}{
		"enabled": true,
	})

	// Disable
	rec := extraDoReq(t, router, "PUT", "/admin/api/header-capture", map[string]interface{}{
		"enabled": false,
	})
	assert.Equal(t, http.StatusOK, rec.Code)
	resp := extraDecodeMap(t, rec)
	assert.Equal(t, false, resp["enabled"])

	// Verify via GET
	rec = extraDoReq(t, router, "GET", "/admin/api/header-capture", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	resp = extraDecodeMap(t, rec)
	assert.Equal(t, false, resp["enabled"])
}

func TestUpdateHeaderCapture_MissingEnabled(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	rec := extraDoReq(t, router, "PUT", "/admin/api/header-capture", map[string]interface{}{})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestClearHeaderCapture(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	// Enable capture (so the state is non-default)
	extraDoReq(t, router, "PUT", "/admin/api/header-capture", map[string]interface{}{
		"enabled": true,
	})

	// Clear
	rec := extraDoReq(t, router, "DELETE", "/admin/api/header-capture", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	resp := extraDecodeMap(t, rec)
	assert.Equal(t, "cleared", resp["status"])

	// Verify captures are empty but enabled flag is preserved
	rec = extraDoReq(t, router, "GET", "/admin/api/header-capture", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	resp = extraDecodeMap(t, rec)
	assert.Equal(t, true, resp["enabled"], "clear should not change enabled flag")
	captures, ok := resp["captures"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, captures)
}

func TestHeaderCaptureLifecycle(t *testing.T) {
	_, router, _ := setupExtraTestAdmin(t)

	// 1. Default: disabled, empty
	rec := extraDoReq(t, router, "GET", "/admin/api/header-capture", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	resp := extraDecodeMap(t, rec)
	assert.Equal(t, false, resp["enabled"])

	// 2. Enable
	rec = extraDoReq(t, router, "PUT", "/admin/api/header-capture", map[string]interface{}{
		"enabled": true,
	})
	require.Equal(t, http.StatusOK, rec.Code)

	// 3. Verify enabled
	rec = extraDoReq(t, router, "GET", "/admin/api/header-capture", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	resp = extraDecodeMap(t, rec)
	assert.Equal(t, true, resp["enabled"])

	// 4. Clear captures
	rec = extraDoReq(t, router, "DELETE", "/admin/api/header-capture", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	// 5. Disable
	rec = extraDoReq(t, router, "PUT", "/admin/api/header-capture", map[string]interface{}{
		"enabled": false,
	})
	require.Equal(t, http.StatusOK, rec.Code)

	// 6. Verify final state: disabled, empty
	rec = extraDoReq(t, router, "GET", "/admin/api/header-capture", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	resp = extraDecodeMap(t, rec)
	assert.Equal(t, false, resp["enabled"])
	captures, ok := resp["captures"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, captures)
}

// TestGetHeaderCapture_NilCapture verifies the handler gracefully handles
// a nil HeaderCapture (returns enabled=false, empty captures).
func TestGetHeaderCapture_NilCapture(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nil_hc.db")
	encKey := []byte("01234567890123456789012345678901")
	s, err := store.NewStore(dbPath, encKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	kc := middleware.NewKeyCache()
	require.NoError(t, kc.Reload(s))
	dp := proxy.NewDynamicProxy()
	prober := proxy.NewUpstreamProber(s, dp, 1*time.Hour, 5*time.Second)
	rc := middleware.NewGlobalRequestCounter()

	// Pass nil for headerCapture
	h := NewAdminHandler(s, kc, middleware.NewPerKeyRPMLimiter(), prober, dp,
		nil, middleware.NewModelFilter(s), rc, middleware.NewPerKeyStatsCollector(),
		middleware.NewModelOverrideCache(s), middleware.NewBindingCache(s),
		nil, // nil HeaderCapture
		nil, // nil CircuitBreaker
		testAdminToken, "test")
	r := mux.NewRouter()
	h.RegisterRoutes(r)

	rec := extraDoReq(t, r, "GET", "/admin/api/header-capture", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	resp := extraDecodeMap(t, rec)
	assert.Equal(t, false, resp["enabled"])
}
