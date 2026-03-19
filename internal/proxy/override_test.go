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
// matchModelOverrides
// ---------------------------------------------------------------------------

func TestMatchModelOverrides_ExactMatch(t *testing.T) {
	overrides := []KeyModelOverrideRule{
		{ModelPattern: "claude-*", UpstreamIDs: []int64{1}},
		{ModelPattern: "claude-opus-4-6", UpstreamIDs: []int64{2}},
	}
	ids := matchModelOverrides(overrides, "claude-opus-4-6")
	assert.Equal(t, []int64{2}, ids, "exact match should win over wildcard")
}

func TestMatchModelOverrides_WildcardMatch(t *testing.T) {
	overrides := []KeyModelOverrideRule{
		{ModelPattern: "claude-*", UpstreamIDs: []int64{1}},
	}
	ids := matchModelOverrides(overrides, "claude-sonnet-4-20250514")
	assert.Equal(t, []int64{1}, ids)
}

func TestMatchModelOverrides_NoMatch(t *testing.T) {
	overrides := []KeyModelOverrideRule{
		{ModelPattern: "claude-*", UpstreamIDs: []int64{1}},
	}
	ids := matchModelOverrides(overrides, "gpt-4o")
	assert.Nil(t, ids, "no match should return nil")
}

func TestMatchModelOverrides_MostSpecificWildcard(t *testing.T) {
	overrides := []KeyModelOverrideRule{
		{ModelPattern: "claude-*", UpstreamIDs: []int64{1}},
		{ModelPattern: "claude-opus-*", UpstreamIDs: []int64{2}},
	}
	ids := matchModelOverrides(overrides, "claude-opus-4-6")
	assert.Equal(t, []int64{2}, ids, "longer/more specific pattern should win")
}

func TestMatchModelOverrides_ExactOverMultipleWildcards(t *testing.T) {
	overrides := []KeyModelOverrideRule{
		{ModelPattern: "claude-*", UpstreamIDs: []int64{1}},
		{ModelPattern: "claude-opus-*", UpstreamIDs: []int64{2}},
		{ModelPattern: "claude-opus-4-6", UpstreamIDs: []int64{3}},
	}
	ids := matchModelOverrides(overrides, "claude-opus-4-6")
	assert.Equal(t, []int64{3}, ids, "exact match always wins")
}

func TestMatchModelOverrides_MultipleUpstreamIDs(t *testing.T) {
	overrides := []KeyModelOverrideRule{
		{ModelPattern: "claude-opus-4-6", UpstreamIDs: []int64{2, 3}},
	}
	ids := matchModelOverrides(overrides, "claude-opus-4-6")
	assert.Equal(t, []int64{2, 3}, ids, "should return all upstream IDs for the match")
}

func TestMatchModelOverrides_Empty(t *testing.T) {
	ids := matchModelOverrides(nil, "claude-opus-4-6")
	assert.Nil(t, ids)

	ids = matchModelOverrides([]KeyModelOverrideRule{}, "claude-opus-4-6")
	assert.Nil(t, ids)
}

// ---------------------------------------------------------------------------
// DynamicProxy whitelist enforcement
// ---------------------------------------------------------------------------

func TestDynamicProxy_WhitelistBlocks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{{ID: 1, BaseURL: parsed, APIKey: "k", Name: "up1"}})
	dp.WhitelistMatcher = func(model string) bool {
		return model == "allowed-model"
	}

	// Blocked model
	body := []byte(`{"model":"blocked-model","messages":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	var errResp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	errObj := errResp["error"].(map[string]interface{})
	assert.Equal(t, "model_not_allowed", errObj["code"])
}

func TestDynamicProxy_WhitelistAllows(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{{ID: 1, BaseURL: parsed, APIKey: "k", Name: "up1"}})
	dp.WhitelistMatcher = func(model string) bool {
		return model == "allowed-model"
	}

	// Allowed model
	body := []byte(`{"model":"allowed-model","messages":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestDynamicProxy_WhitelistNilMatcher_PassesAll(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{{ID: 1, BaseURL: parsed, APIKey: "k", Name: "up1"}})
	// WhitelistMatcher is nil  - should allow all

	body := []byte(`{"model":"anything","messages":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestDynamicProxy_WhitelistNonJSON_Passes(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{{ID: 1, BaseURL: parsed, APIKey: "k", Name: "up1"}})
	dp.WhitelistMatcher = func(model string) bool {
		return false // block everything
	}

	// Non-JSON body should pass through
	body := []byte("not json")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "non-JSON should bypass whitelist")
}

// ---------------------------------------------------------------------------
// DynamicProxy per-key model override integration
// ---------------------------------------------------------------------------

func TestDynamicProxy_OverrideRoutesToSpecificUpstream(t *testing.T) {
	called := ""
	upA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = "A"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer upA.Close()

	upB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = "B"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer upB.Close()

	dp := NewDynamicProxy()
	parsedA, _ := url.Parse(upA.URL)
	parsedB, _ := url.Parse(upB.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsedA, APIKey: "k1", Name: "A"},
		{ID: 2, BaseURL: parsedB, APIKey: "k2", Name: "B"},
	})

	// Set override: claude-opus-4-6 -> upstream B (ID=2)
	overrides := []KeyModelOverrideRule{
		{ModelPattern: "claude-opus-4-6", UpstreamIDs: []int64{2}},
	}

	body := []byte(`{"model":"claude-opus-4-6"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := ContextWithKeyModelOverrides(req.Context(), overrides)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "B", called, "should route to B per override")
}

func TestDynamicProxy_OverrideHardFail(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	dp := NewDynamicProxy()
	parsed, _ := url.Parse(upstream.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsed, APIKey: "k", Name: "up1"},
	})

	// Override says go to upstream 99, which doesn't exist in healthy list
	overrides := []KeyModelOverrideRule{
		{ModelPattern: "claude-opus-4-6", UpstreamIDs: []int64{99}},
	}

	body := []byte(`{"model":"claude-opus-4-6"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	ctx := ContextWithKeyModelOverrides(req.Context(), overrides)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "should hard fail when override upstream not available")
	var errResp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	errObj := errResp["error"].(map[string]interface{})
	assert.Equal(t, "override_upstream_unavailable", errObj["code"])
}

func TestDynamicProxy_NoOverride_DefaultBehavior(t *testing.T) {
	called := ""
	upA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = "A"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer upA.Close()

	dp := NewDynamicProxy()
	parsedA, _ := url.Parse(upA.URL)
	dp.SetAllUpstreams([]*ActiveUpstream{
		{ID: 1, BaseURL: parsedA, APIKey: "k1", Name: "A"},
	})

	// No override in context
	body := []byte(`{"model":"claude-opus-4-6"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	dp.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "A", called, "should route to A by default")
}

// ---------------------------------------------------------------------------
// Context helpers round-trip
// ---------------------------------------------------------------------------

func TestKeyModelOverrides_RoundTrip(t *testing.T) {
	ctx := context.Background()
	rules := []KeyModelOverrideRule{
		{ModelPattern: "claude-*", UpstreamIDs: []int64{1, 2}},
	}
	ctx = ContextWithKeyModelOverrides(ctx, rules)
	got := KeyModelOverridesFromContext(ctx)
	assert.Equal(t, rules, got)
}

func TestKeyModelOverrides_EmptyContext(t *testing.T) {
	ctx := context.Background()
	got := KeyModelOverridesFromContext(ctx)
	assert.Nil(t, got)
}
