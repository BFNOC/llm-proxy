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

// setupModelsTestAdmin creates an isolated admin handler + router + store for
// model-management tests. It uses a unique name to avoid collisions with other
// test helpers in the package.
func setupModelsTestAdmin(t *testing.T) (*AdminHandler, *mux.Router, *store.Store) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "models_test.db")
	encKey := []byte("01234567890123456789012345678901") // 32 bytes
	s, err := store.NewStore(dbPath, encKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	kc := middleware.NewKeyCache()
	require.NoError(t, kc.Reload(s))

	rl := middleware.NewPerKeyRPMLimiter()
	dp := proxy.NewDynamicProxy()
	prober := proxy.NewUpstreamProber(s, dp, 1*time.Hour, 5*time.Second)
	mf := middleware.NewModelFilter(s)
	rc := middleware.NewGlobalRequestCounter()
	pks := middleware.NewPerKeyStatsCollector()
	oc := middleware.NewModelOverrideCache(s)
	bc := middleware.NewBindingCache(s)
	hc := middleware.NewHeaderCapture(10)

	h := NewAdminHandler(s, kc, rl, prober, dp, nil, mf, rc, pks, oc, bc, hc, testAdminToken, "test")
	r := mux.NewRouter()
	h.RegisterRoutes(r)

	return h, r, s
}

// modelsDoReq builds an authenticated JSON request, executes it, and returns the recorder.
func modelsDoReq(t *testing.T, router *mux.Router, method, path string, body interface{}) *httptest.ResponseRecorder {
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

// modelsDecodeMap decodes the response body into a map.
func modelsDecodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result), "body: %s", rec.Body.String())
	return result
}

// modelsSeedUpstream creates an upstream via the store and returns its ID.
func modelsSeedUpstream(t *testing.T, s *store.Store, name string) int64 {
	t.Helper()
	u, err := s.CreateUpstream(name, "https://api.example.com", []string{"sk-test-key"}, 10, "", "round-robin", "api_key", "", false, false)
	require.NoError(t, err)
	return u.ID
}

// ---------------------------------------------------------------------------
// Upstream Model Patterns: GET/PUT /admin/api/upstreams/{id}/models
//                          GET     /admin/api/upstreams/models
// ---------------------------------------------------------------------------

func TestGetUpstreamModelPatterns_Empty(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "empty-patterns")

	rec := modelsDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := modelsDecodeMap(t, rec)
	patterns, ok := resp["patterns"].([]interface{})
	require.True(t, ok, "patterns should be an array")
	assert.Empty(t, patterns)
}

func TestGetUpstreamModelPatterns_NotFound(t *testing.T) {
	_, router, _ := setupModelsTestAdmin(t)

	rec := modelsDoReq(t, router, "GET", "/admin/api/upstreams/99999/models", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSetAndGetUpstreamModelPatterns(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "set-patterns")

	// PUT patterns
	rec := modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), map[string]interface{}{
		"patterns": []string{"gpt-4*", "claude-*"},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	putResp := modelsDecodeMap(t, rec)
	assert.Equal(t, "updated", putResp["status"])
	putPatterns := putResp["patterns"].([]interface{})
	assert.Len(t, putPatterns, 2)

	// GET to verify
	rec = modelsDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	getResp := modelsDecodeMap(t, rec)
	patterns := getResp["patterns"].([]interface{})
	assert.Len(t, patterns, 2)
	// Verify both patterns are present (order may vary)
	patternStrs := make([]string, len(patterns))
	for i, p := range patterns {
		patternStrs[i] = p.(string)
	}
	assert.Contains(t, patternStrs, "gpt-4*")
	assert.Contains(t, patternStrs, "claude-*")
}

func TestSetUpstreamModelPatterns_ClearPatterns(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "clear-patterns")

	// Set patterns first
	modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), map[string]interface{}{
		"patterns": []string{"gpt-4*", "claude-*"},
	})

	// Clear with empty array
	rec := modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), map[string]interface{}{
		"patterns": []string{},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify empty
	rec = modelsDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := modelsDecodeMap(t, rec)
	patterns := resp["patterns"].([]interface{})
	assert.Empty(t, patterns)
}

func TestSetUpstreamModelPatterns_InvalidGlob(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "invalid-glob")

	rec := modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), map[string]interface{}{
		"patterns": []string{"[invalid"},
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	resp := modelsDecodeMap(t, rec)
	assert.Contains(t, resp["error"], "invalid pattern")
}

func TestSetUpstreamModelPatterns_MissingPatternsField(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "missing-field")

	// Send {} without "patterns" key
	rec := modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), map[string]interface{}{})
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	resp := modelsDecodeMap(t, rec)
	assert.Contains(t, resp["error"], "missing required field")
}

func TestSetUpstreamModelPatterns_NotFound(t *testing.T) {
	_, router, _ := setupModelsTestAdmin(t)

	rec := modelsDoReq(t, router, "PUT", "/admin/api/upstreams/99999/models", map[string]interface{}{
		"patterns": []string{"gpt-*"},
	})
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSetUpstreamModelPatterns_DeduplicatesAndTrims(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "dedup-patterns")

	rec := modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), map[string]interface{}{
		"patterns": []string{"gpt-4*", "  gpt-4*  ", "claude-*", "gpt-4*"},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := modelsDecodeMap(t, rec)
	patterns := resp["patterns"].([]interface{})
	assert.Len(t, patterns, 2, "duplicates and trimmed duplicates should be removed")
}

func TestSetUpstreamModelPatterns_SkipsEmptyStrings(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "empty-strings")

	rec := modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), map[string]interface{}{
		"patterns": []string{"gpt-4*", "", "  ", "claude-*"},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := modelsDecodeMap(t, rec)
	patterns := resp["patterns"].([]interface{})
	assert.Len(t, patterns, 2, "empty and whitespace-only patterns should be skipped")
}

func TestSetUpstreamModelPatterns_ValidPatterns(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
	}{
		{"wildcard all", []string{"*"}},
		{"prefix match", []string{"gpt-4*"}},
		{"suffix match", []string{"*-instruct"}},
		{"middle wildcard", []string{"gpt-*-turbo"}},
		{"character class", []string{"gpt-[34]*"}},
		{"question mark", []string{"gpt-?-turbo"}},
		{"multiple valid", []string{"gpt-*", "claude-*", "llama-3-*"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, router, s := setupModelsTestAdmin(t)
			uID := modelsSeedUpstream(t, s, "valid-"+tt.name)

			rec := modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), map[string]interface{}{
				"patterns": tt.patterns,
			})
			assert.Equal(t, http.StatusOK, rec.Code, "patterns %v should be accepted", tt.patterns)
		})
	}
}

func TestSetUpstreamModelPatterns_InvalidJSON(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "bad-json")

	req := httptest.NewRequest("PUT", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), bytes.NewBufferString("not json"))
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// All Upstream Model Patterns: GET /admin/api/upstreams/models
// ---------------------------------------------------------------------------

func TestGetAllUpstreamModelPatterns_Empty(t *testing.T) {
	_, router, _ := setupModelsTestAdmin(t)

	rec := modelsDoReq(t, router, "GET", "/admin/api/upstreams/models", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Should return a valid map (possibly empty), never null
	resp := modelsDecodeMap(t, rec)
	assert.NotNil(t, resp)
}

func TestGetAllUpstreamModelPatterns_WithData(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	u1 := modelsSeedUpstream(t, s, "all-patterns-u1")
	u2 := modelsSeedUpstream(t, s, "all-patterns-u2")

	// Set patterns on two upstreams
	require.NoError(t, s.SetUpstreamModelPatterns(u1, []string{"gpt-4*"}))
	require.NoError(t, s.SetUpstreamModelPatterns(u2, []string{"claude-*", "llama-*"}))

	rec := modelsDoReq(t, router, "GET", "/admin/api/upstreams/models", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := modelsDecodeMap(t, rec)
	// Both upstream IDs should be present as keys (JSON keys are strings)
	assert.Contains(t, resp, fmt.Sprintf("%d", u1))
	assert.Contains(t, resp, fmt.Sprintf("%d", u2))

	// Verify counts
	u1Patterns := resp[fmt.Sprintf("%d", u1)].([]interface{})
	assert.Len(t, u1Patterns, 1)
	u2Patterns := resp[fmt.Sprintf("%d", u2)].([]interface{})
	assert.Len(t, u2Patterns, 2)
}

func TestGetAllUpstreamModelPatterns_RequiresAuth(t *testing.T) {
	_, router, _ := setupModelsTestAdmin(t)

	req := httptest.NewRequest("GET", "/admin/api/upstreams/models", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ---------------------------------------------------------------------------
// Upstream Model Patterns: full lifecycle
// ---------------------------------------------------------------------------

func TestUpstreamModelPatterns_Lifecycle(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "lifecycle-patterns")

	// 1. Initially empty
	rec := modelsDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	resp := modelsDecodeMap(t, rec)
	assert.Empty(t, resp["patterns"].([]interface{}))

	// 2. Set patterns
	rec = modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), map[string]interface{}{
		"patterns": []string{"gpt-4*", "claude-3*"},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	// 3. Verify set
	rec = modelsDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), nil)
	resp = modelsDecodeMap(t, rec)
	assert.Len(t, resp["patterns"].([]interface{}), 2)

	// 4. Replace patterns (full overwrite)
	rec = modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), map[string]interface{}{
		"patterns": []string{"llama-*"},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	// 5. Verify replaced
	rec = modelsDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), nil)
	resp = modelsDecodeMap(t, rec)
	patterns := resp["patterns"].([]interface{})
	assert.Len(t, patterns, 1)
	assert.Equal(t, "llama-*", patterns[0])

	// 6. Clear
	rec = modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), map[string]interface{}{
		"patterns": []string{},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	// 7. Verify cleared
	rec = modelsDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/models", uID), nil)
	resp = modelsDecodeMap(t, rec)
	assert.Empty(t, resp["patterns"].([]interface{}))
}

// ---------------------------------------------------------------------------
// Upstream Declared Models: GET/PUT /admin/api/upstreams/{id}/declared-models
//                           GET     /admin/api/upstreams/declared-models
// ---------------------------------------------------------------------------

func TestGetUpstreamDeclaredModels_Empty(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "empty-declared")

	rec := modelsDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := modelsDecodeMap(t, rec)
	models, ok := resp["models"].([]interface{})
	require.True(t, ok, "models should be an array")
	assert.Empty(t, models)
}

func TestGetUpstreamDeclaredModels_NotFound(t *testing.T) {
	_, router, _ := setupModelsTestAdmin(t)

	rec := modelsDoReq(t, router, "GET", "/admin/api/upstreams/99999/declared-models", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSetAndGetUpstreamDeclaredModels(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "set-declared")

	// PUT declared models
	rec := modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), map[string]interface{}{
		"models": []string{"gpt-4o", "gpt-4o-mini", "claude-3-5-sonnet-20241022"},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	putResp := modelsDecodeMap(t, rec)
	assert.Equal(t, "updated", putResp["status"])
	putModels := putResp["models"].([]interface{})
	assert.Len(t, putModels, 3)

	// GET to verify
	rec = modelsDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	getResp := modelsDecodeMap(t, rec)
	models := getResp["models"].([]interface{})
	assert.Len(t, models, 3)
	modelStrs := make([]string, len(models))
	for i, m := range models {
		modelStrs[i] = m.(string)
	}
	assert.Contains(t, modelStrs, "gpt-4o")
	assert.Contains(t, modelStrs, "gpt-4o-mini")
	assert.Contains(t, modelStrs, "claude-3-5-sonnet-20241022")
}

func TestSetUpstreamDeclaredModels_ClearModels(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "clear-declared")

	// Set first
	modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), map[string]interface{}{
		"models": []string{"gpt-4o", "gpt-4o-mini"},
	})

	// Clear with empty array
	rec := modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), map[string]interface{}{
		"models": []string{},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify empty
	rec = modelsDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := modelsDecodeMap(t, rec)
	models := resp["models"].([]interface{})
	assert.Empty(t, models)
}

func TestSetUpstreamDeclaredModels_MissingModelsField(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "missing-models-field")

	// Send {} without "models" key
	rec := modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), map[string]interface{}{})
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	resp := modelsDecodeMap(t, rec)
	assert.Contains(t, resp["error"], "missing required field")
}

func TestSetUpstreamDeclaredModels_NotFound(t *testing.T) {
	_, router, _ := setupModelsTestAdmin(t)

	rec := modelsDoReq(t, router, "PUT", "/admin/api/upstreams/99999/declared-models", map[string]interface{}{
		"models": []string{"gpt-4o"},
	})
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSetUpstreamDeclaredModels_DeduplicatesAndTrims(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "dedup-declared")

	rec := modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), map[string]interface{}{
		"models": []string{"gpt-4o", "  gpt-4o  ", "claude-3-opus", "gpt-4o"},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := modelsDecodeMap(t, rec)
	models := resp["models"].([]interface{})
	assert.Len(t, models, 2, "duplicates and trimmed duplicates should be removed")
}

func TestSetUpstreamDeclaredModels_SkipsEmptyStrings(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "empty-strings-declared")

	rec := modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), map[string]interface{}{
		"models": []string{"gpt-4o", "", "  ", "claude-3-opus"},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := modelsDecodeMap(t, rec)
	models := resp["models"].([]interface{})
	assert.Len(t, models, 2, "empty and whitespace-only model IDs should be skipped")
}

func TestSetUpstreamDeclaredModels_InvalidJSON(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "bad-json-declared")

	req := httptest.NewRequest("PUT", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), bytes.NewBufferString("not json"))
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// All Upstream Declared Models: GET /admin/api/upstreams/declared-models
// ---------------------------------------------------------------------------

func TestGetAllUpstreamDeclaredModels_Empty(t *testing.T) {
	_, router, _ := setupModelsTestAdmin(t)

	rec := modelsDoReq(t, router, "GET", "/admin/api/upstreams/declared-models", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := modelsDecodeMap(t, rec)
	assert.NotNil(t, resp)
}

func TestGetAllUpstreamDeclaredModels_WithData(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	u1 := modelsSeedUpstream(t, s, "all-declared-u1")
	u2 := modelsSeedUpstream(t, s, "all-declared-u2")

	// Set declared models on two upstreams
	require.NoError(t, s.SetUpstreamDeclaredModels(u1, []string{"gpt-4o"}))
	require.NoError(t, s.SetUpstreamDeclaredModels(u2, []string{"claude-3-opus", "claude-3-sonnet"}))

	rec := modelsDoReq(t, router, "GET", "/admin/api/upstreams/declared-models", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := modelsDecodeMap(t, rec)
	// Both upstream IDs should be present (GetAllUpstreamDeclaredModels only returns enabled upstreams)
	assert.Contains(t, resp, fmt.Sprintf("%d", u1))
	assert.Contains(t, resp, fmt.Sprintf("%d", u2))

	u1Models := resp[fmt.Sprintf("%d", u1)].([]interface{})
	assert.Len(t, u1Models, 1)
	u2Models := resp[fmt.Sprintf("%d", u2)].([]interface{})
	assert.Len(t, u2Models, 2)
}

func TestGetAllUpstreamDeclaredModels_RequiresAuth(t *testing.T) {
	_, router, _ := setupModelsTestAdmin(t)

	req := httptest.NewRequest("GET", "/admin/api/upstreams/declared-models", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ---------------------------------------------------------------------------
// Upstream Declared Models: full lifecycle
// ---------------------------------------------------------------------------

func TestUpstreamDeclaredModels_Lifecycle(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "lifecycle-declared")

	// 1. Initially empty
	rec := modelsDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	resp := modelsDecodeMap(t, rec)
	assert.Empty(t, resp["models"].([]interface{}))

	// 2. Set models
	rec = modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), map[string]interface{}{
		"models": []string{"gpt-4o", "gpt-4o-mini"},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	// 3. Verify set
	rec = modelsDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), nil)
	resp = modelsDecodeMap(t, rec)
	assert.Len(t, resp["models"].([]interface{}), 2)

	// 4. Replace models (full overwrite)
	rec = modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), map[string]interface{}{
		"models": []string{"claude-3-5-sonnet-20241022"},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	// 5. Verify replaced
	rec = modelsDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), nil)
	resp = modelsDecodeMap(t, rec)
	models := resp["models"].([]interface{})
	assert.Len(t, models, 1)
	assert.Equal(t, "claude-3-5-sonnet-20241022", models[0])

	// 6. Clear
	rec = modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), map[string]interface{}{
		"models": []string{},
	})
	assert.Equal(t, http.StatusOK, rec.Code)

	// 7. Verify cleared
	rec = modelsDoReq(t, router, "GET", fmt.Sprintf("/admin/api/upstreams/%d/declared-models", uID), nil)
	resp = modelsDecodeMap(t, rec)
	assert.Empty(t, resp["models"].([]interface{}))
}

// ---------------------------------------------------------------------------
// validateProxyURL — tested indirectly via createUpstream
// ---------------------------------------------------------------------------

func TestCreateUpstream_ProxyURL_Validation(t *testing.T) {
	tests := []struct {
		name       string
		proxyURL   string
		wantStatus int
	}{
		{
			name:       "valid http proxy",
			proxyURL:   "http://proxy:8080",
			wantStatus: http.StatusCreated,
		},
		{
			name:       "valid https proxy",
			proxyURL:   "https://proxy:443",
			wantStatus: http.StatusCreated,
		},
		{
			name:       "valid socks5 proxy",
			proxyURL:   "socks5://proxy:1080",
			wantStatus: http.StatusCreated,
		},
		{
			name:       "invalid ftp scheme",
			proxyURL:   "ftp://bad:21",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing scheme",
			proxyURL:   "://no-scheme",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty hostname with scheme",
			proxyURL:   "http://",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, router, _ := setupModelsTestAdmin(t)
			rec := modelsDoReq(t, router, "POST", "/admin/api/upstreams", map[string]interface{}{
				"name":      "proxy-test-" + tt.name,
				"base_url":  "https://api.openai.com",
				"proxy_url": tt.proxyURL,
			})
			assert.Equal(t, tt.wantStatus, rec.Code, "proxy_url=%q", tt.proxyURL)

			if tt.wantStatus == http.StatusBadRequest {
				resp := modelsDecodeMap(t, rec)
				assert.Contains(t, resp, "error")
			}
		})
	}
}

func TestUpdateUpstream_ProxyURL_Validation(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)
	uID := modelsSeedUpstream(t, s, "proxy-update-test")

	tests := []struct {
		name       string
		proxyURL   string
		wantStatus int
	}{
		{
			name:       "valid http proxy update",
			proxyURL:   "http://new-proxy:8080",
			wantStatus: http.StatusOK,
		},
		{
			name:       "valid socks5 proxy update",
			proxyURL:   "socks5://socks-proxy:1080",
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid scheme update",
			proxyURL:   "ftp://bad-proxy:21",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d", uID), map[string]interface{}{
				"proxy_url": tt.proxyURL,
			})
			assert.Equal(t, tt.wantStatus, rec.Code, "proxy_url=%q", tt.proxyURL)
		})
	}
}

func TestCreateUpstream_ProxyURL_ClearOnUpdate(t *testing.T) {
	_, router, s := setupModelsTestAdmin(t)

	// Create with a proxy
	u, err := s.CreateUpstream("proxy-clear-test", "https://api.example.com", []string{"sk-key"}, 10, "http://proxy:8080", "round-robin", "api_key", "", false, false)
	require.NoError(t, err)

	// Update to clear proxy (empty string should be accepted)
	rec := modelsDoReq(t, router, "PUT", fmt.Sprintf("/admin/api/upstreams/%d", u.ID), map[string]interface{}{
		"proxy_url": "",
	})
	assert.Equal(t, http.StatusOK, rec.Code)
}
