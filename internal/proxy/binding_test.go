package proxy

import (
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
