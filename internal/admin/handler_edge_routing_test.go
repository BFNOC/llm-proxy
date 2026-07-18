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
