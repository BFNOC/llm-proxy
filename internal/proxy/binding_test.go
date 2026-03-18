package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Context helpers round-trip
// ---------------------------------------------------------------------------

func TestAllowedUpstreamIDs_RoundTrip(t *testing.T) {
	ctx := context.Background()
	ids := []int64{10, 20, 30}

	ctx = ContextWithAllowedUpstreamIDs(ctx, ids)
	got := AllowedUpstreamIDsFromContext(ctx)
	assert.Equal(t, ids, got)
}

func TestAllowedUpstreamIDs_EmptyContext_ReturnsNil(t *testing.T) {
	ctx := context.Background()
	got := AllowedUpstreamIDsFromContext(ctx)
	assert.Nil(t, got)
}

func TestAllowedUpstreamIDs_EmptySlice(t *testing.T) {
	ctx := context.Background()
	ctx = ContextWithAllowedUpstreamIDs(ctx, []int64{})
	got := AllowedUpstreamIDsFromContext(ctx)
	assert.Empty(t, got)
}

// ---------------------------------------------------------------------------
// filterUpstreams
// ---------------------------------------------------------------------------

func makeUpstream(id int64, name string) *ActiveUpstream {
	u, _ := url.Parse("https://" + name + ".example.com")
	return &ActiveUpstream{ID: id, BaseURL: u, APIKey: "key", Name: name}
}

func TestFilterUpstreams_SubsetMatch(t *testing.T) {
	all := []*ActiveUpstream{makeUpstream(1, "a"), makeUpstream(2, "b"), makeUpstream(3, "c")}
	result := filterUpstreams(all, []int64{2, 3})
	require.Len(t, result, 2)
	assert.Equal(t, int64(2), result[0].ID)
	assert.Equal(t, int64(3), result[1].ID)
}

func TestFilterUpstreams_NoMatch(t *testing.T) {
	all := []*ActiveUpstream{makeUpstream(1, "a"), makeUpstream(2, "b")}
	result := filterUpstreams(all, []int64{99})
	assert.Empty(t, result)
}

func TestFilterUpstreams_AllMatch(t *testing.T) {
	all := []*ActiveUpstream{makeUpstream(1, "a"), makeUpstream(2, "b")}
	result := filterUpstreams(all, []int64{1, 2})
	assert.Len(t, result, 2)
}

func TestFilterUpstreams_PreservesOrder(t *testing.T) {
	all := []*ActiveUpstream{makeUpstream(3, "c"), makeUpstream(1, "a"), makeUpstream(2, "b")}
	result := filterUpstreams(all, []int64{2, 3}) // IDs in different order from all
	require.Len(t, result, 2)
	assert.Equal(t, int64(3), result[0].ID, "should follow order of 'all', not 'allowedIDs'")
	assert.Equal(t, int64(2), result[1].ID)
}

func TestFilterUpstreams_DuplicateAllowedIDs(t *testing.T) {
	all := []*ActiveUpstream{makeUpstream(1, "a"), makeUpstream(2, "b")}
	result := filterUpstreams(all, []int64{1, 1, 1})
	assert.Len(t, result, 1, "duplicate allowed IDs should not duplicate results")
}

func TestFilterUpstreams_NilAll(t *testing.T) {
	result := filterUpstreams(nil, []int64{1, 2})
	assert.Empty(t, result)
}

func TestFilterUpstreams_EmptyAllowedIDs(t *testing.T) {
	all := []*ActiveUpstream{makeUpstream(1, "a")}
	result := filterUpstreams(all, []int64{})
	assert.Empty(t, result, "empty allowed list means no match")
}

// ---------------------------------------------------------------------------
// DynamicProxy binding integration
// ---------------------------------------------------------------------------

func TestDynamicProxy_BindingFilter_Returns403WhenNoMatch(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{{ID: 1, BaseURL: parsed, APIKey: "k", Name: "up1"}})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	// Set allowed IDs to non-existent upstream
	ctx := ContextWithAllowedUpstreamIDs(req.Context(), []int64{999})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body["error"], "no permitted upstream")
}

func TestDynamicProxy_BindingFilter_AllowsMatchingUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{{ID: 42, BaseURL: parsed, APIKey: "k", Name: "up42"}})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx := ContextWithAllowedUpstreamIDs(req.Context(), []int64{42})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestDynamicProxy_NoBinding_UsesAllUpstreams(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{{ID: 1, BaseURL: parsed, APIKey: "k", Name: "up1"}})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	// No binding set in context
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "should use all upstreams when no binding")
}

// ---------------------------------------------------------------------------
// extractModelFromBody
// ---------------------------------------------------------------------------

func TestExtractModelFromBody_ValidJSON(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-20250514","messages":[]}`)
	model, isJSON := extractModelFromBody(body)
	assert.True(t, isJSON)
	assert.Equal(t, "claude-sonnet-4-20250514", model)
}

func TestExtractModelFromBody_EmptyBody(t *testing.T) {
	model, isJSON := extractModelFromBody(nil)
	assert.False(t, isJSON)
	assert.Equal(t, "", model)

	model, isJSON = extractModelFromBody([]byte{})
	assert.False(t, isJSON)
	assert.Equal(t, "", model)
}

func TestExtractModelFromBody_NonJSON(t *testing.T) {
	model, isJSON := extractModelFromBody([]byte("not json"))
	assert.False(t, isJSON, "non-JSON body should return isJSON=false")
	assert.Equal(t, "", model)
}

func TestExtractModelFromBody_NoModelKey(t *testing.T) {
	model, isJSON := extractModelFromBody([]byte(`{"prompt":"hello"}`))
	assert.True(t, isJSON, "valid JSON should return isJSON=true")
	assert.Equal(t, "", model, "missing model key should return empty string")
}

func TestExtractModelFromBody_ModelNull(t *testing.T) {
	model, isJSON := extractModelFromBody([]byte(`{"model":null}`))
	assert.True(t, isJSON, "JSON with null model should be valid JSON")
	assert.Equal(t, "", model, "null model should return empty string")
}

func TestExtractModelFromBody_ModelNumber(t *testing.T) {
	model, isJSON := extractModelFromBody([]byte(`{"model":123}`))
	assert.True(t, isJSON, "JSON with numeric model should be valid JSON")
	assert.Equal(t, "", model, "numeric model should return empty string")
}

func TestExtractModelFromBody_ArrayBody(t *testing.T) {
	model, isJSON := extractModelFromBody([]byte(`[1,2,3]`))
	assert.False(t, isJSON, "JSON array can't unmarshal to struct, treated as non-JSON")
	assert.Equal(t, "", model)
}

// ---------------------------------------------------------------------------
// filterUpstreamsByModel
// ---------------------------------------------------------------------------

func makeUpstreamWithPatterns(id int64, name string, patterns []string) *ActiveUpstream {
	u, _ := url.Parse("https://" + name + ".example.com")
	return &ActiveUpstream{ID: id, BaseURL: u, APIKey: "key", Name: name, ModelPatterns: patterns}
}

func TestFilterUpstreamsByModel_MatchesPattern(t *testing.T) {
	all := []*ActiveUpstream{
		makeUpstreamWithPatterns(1, "claude-provider", []string{"claude-*"}),
		makeUpstreamWithPatterns(2, "gpt-provider", []string{"gpt-*"}),
	}
	result := filterUpstreamsByModel(all, "claude-sonnet-4-20250514")
	require.Len(t, result, 1)
	assert.Equal(t, int64(1), result[0].ID)
}

func TestFilterUpstreamsByModel_NoMatch(t *testing.T) {
	all := []*ActiveUpstream{
		makeUpstreamWithPatterns(1, "claude-only", []string{"claude-*"}),
	}
	result := filterUpstreamsByModel(all, "gpt-4o")
	assert.Empty(t, result)
}

func TestFilterUpstreamsByModel_NoPatternsAcceptsAll(t *testing.T) {
	all := []*ActiveUpstream{
		makeUpstreamWithPatterns(1, "all-models", nil),
		makeUpstreamWithPatterns(2, "claude-only", []string{"claude-*"}),
	}
	result := filterUpstreamsByModel(all, "gpt-4o")
	require.Len(t, result, 1, "only the no-pattern upstream should match")
	assert.Equal(t, int64(1), result[0].ID)
}

func TestFilterUpstreamsByModel_MixedUpstreams(t *testing.T) {
	all := []*ActiveUpstream{
		makeUpstreamWithPatterns(1, "claude", []string{"claude-*"}),
		makeUpstreamWithPatterns(2, "gpt", []string{"gpt-*", "o1-*"}),
		makeUpstreamWithPatterns(3, "wildcard", nil),
	}
	// claude model should match upstream 1 and 3
	result := filterUpstreamsByModel(all, "claude-3-opus")
	require.Len(t, result, 2)
	assert.Equal(t, int64(1), result[0].ID)
	assert.Equal(t, int64(3), result[1].ID)
}

func TestFilterUpstreamsByModel_MultiplePatterns(t *testing.T) {
	all := []*ActiveUpstream{
		makeUpstreamWithPatterns(1, "multi", []string{"gpt-*", "o1-*"}),
	}
	assert.Len(t, filterUpstreamsByModel(all, "gpt-4o"), 1)
	assert.Len(t, filterUpstreamsByModel(all, "o1-mini"), 1)
	assert.Empty(t, filterUpstreamsByModel(all, "claude-3"))
}

// ---------------------------------------------------------------------------
// DynamicProxy model routing integration
// ---------------------------------------------------------------------------

func TestDynamicProxy_ModelRouting_Returns422WhenNoMatch(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed, APIKey: "k", Name: "claude-only", ModelPatterns: []string{"claude-*"}},
	})

	body := []byte(`{"model":"gpt-4o","messages":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var errResp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	errObj := errResp["error"].(map[string]interface{})
	assert.Contains(t, errObj["message"], "gpt-4o")
	assert.Equal(t, "model_not_available", errObj["code"])
}

func TestDynamicProxy_ModelRouting_RoutesToMatchingUpstream(t *testing.T) {
	called := ""
	claudeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = "claude"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer claudeServer.Close()

	gptServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = "gpt"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer gptServer.Close()

	dp := NewDynamicProxy()
	claudeURL, _ := url.Parse(claudeServer.URL)
	gptURL, _ := url.Parse(gptServer.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: claudeURL, APIKey: "k1", Name: "claude-up", ModelPatterns: []string{"claude-*"}},
		{ID: 2, BaseURL: gptURL, APIKey: "k2", Name: "gpt-up", ModelPatterns: []string{"gpt-*"}},
	})

	// Request with claude model
	body := []byte(`{"model":"claude-sonnet-4-20250514"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "claude", called)
}

func TestDynamicProxy_ModelRouting_GETSkipsFilter(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"object":"list","data":[]}`)) //nolint:errcheck
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed, APIKey: "k", Name: "up", ModelPatterns: []string{"claude-*"}},
	})

	// GET /v1/models should NOT be filtered by model patterns
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "GET requests should skip model filtering")
}

