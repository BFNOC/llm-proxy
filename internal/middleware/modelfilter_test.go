package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newModelFilterStore creates a temporary store for model filter tests.
func newModelFilterStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	encKey := []byte("01234567890123456789012345678901")
	s, err := store.NewStore(filepath.Join(dir, "mf.db"), encKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// ---------------------------------------------------------------------------
// MatchModel
// ---------------------------------------------------------------------------

func TestMatchModel_EmptyWhitelist_AllowsAll(t *testing.T) {
	s := newModelFilterStore(t)
	mf := NewModelFilter(s)

	assert.True(t, mf.MatchModel("gpt-4"))
	assert.True(t, mf.MatchModel("claude-3-opus"))
	assert.True(t, mf.MatchModel("anything"))
}

func TestMatchModel_ExactMatch(t *testing.T) {
	s := newModelFilterStore(t)
	_, err := s.AddModelWhitelist("gpt-4")
	require.NoError(t, err)

	mf := NewModelFilter(s)

	assert.True(t, mf.MatchModel("gpt-4"))
	assert.False(t, mf.MatchModel("gpt-4-turbo"))
	assert.False(t, mf.MatchModel("gpt-3.5-turbo"))
}

func TestMatchModel_GlobPattern(t *testing.T) {
	s := newModelFilterStore(t)
	_, err := s.AddModelWhitelist("gpt-4*")
	require.NoError(t, err)

	mf := NewModelFilter(s)

	assert.True(t, mf.MatchModel("gpt-4"))
	assert.True(t, mf.MatchModel("gpt-4-turbo"))
	assert.True(t, mf.MatchModel("gpt-4o"))
	assert.False(t, mf.MatchModel("gpt-3.5-turbo"))
}

func TestMatchModel_QuestionMarkGlob(t *testing.T) {
	s := newModelFilterStore(t)
	_, err := s.AddModelWhitelist("gpt-?")
	require.NoError(t, err)

	mf := NewModelFilter(s)

	assert.True(t, mf.MatchModel("gpt-4"))
	assert.True(t, mf.MatchModel("gpt-3"))
	assert.False(t, mf.MatchModel("gpt-4o"))
	assert.False(t, mf.MatchModel("gpt-35"))
}

func TestMatchModel_MultiplePatterns(t *testing.T) {
	s := newModelFilterStore(t)
	_, err := s.AddModelWhitelist("gpt-4*")
	require.NoError(t, err)
	_, err = s.AddModelWhitelist("claude-*")
	require.NoError(t, err)

	mf := NewModelFilter(s)

	assert.True(t, mf.MatchModel("gpt-4-turbo"))
	assert.True(t, mf.MatchModel("claude-3-opus"))
	assert.False(t, mf.MatchModel("llama-2-70b"))
}

func TestMatchModel_NonMatching(t *testing.T) {
	s := newModelFilterStore(t)
	_, err := s.AddModelWhitelist("gpt-4")
	require.NoError(t, err)

	mf := NewModelFilter(s)

	assert.False(t, mf.MatchModel("claude-3"))
	assert.False(t, mf.MatchModel(""))
}

// ---------------------------------------------------------------------------
// Reload
// ---------------------------------------------------------------------------

func TestModelFilter_Reload(t *testing.T) {
	s := newModelFilterStore(t)
	mf := NewModelFilter(s)

	// Initially empty -> allows all.
	assert.True(t, mf.MatchModel("gpt-4"))

	// Add a pattern and reload.
	_, err := s.AddModelWhitelist("claude-*")
	require.NoError(t, err)
	mf.Reload()

	assert.True(t, mf.MatchModel("claude-3-opus"))
	assert.False(t, mf.MatchModel("gpt-4"), "after reload with whitelist, non-matching models should be rejected")
}

// ---------------------------------------------------------------------------
// ReloadDeclaredModels
// ---------------------------------------------------------------------------

func TestModelFilter_ReloadDeclaredModels(t *testing.T) {
	s := newModelFilterStore(t)
	mf := NewModelFilter(s)

	// Initially no declared models.
	dm := mf.getDeclaredModels()
	assert.Empty(t, dm)

	// Create an upstream so we can insert declared models.
	up, err := s.CreateUpstream("test-up", "https://api.example.com", []string{"sk-test"}, 0, "", "", "", "", false, false)
	require.NoError(t, err)

	// Use the store's SetUpstreamDeclaredModels method.
	err = s.SetUpstreamDeclaredModels(up.ID, []string{"declared-model-1", "declared-model-2"})
	require.NoError(t, err)

	mf.ReloadDeclaredModels()

	dm = mf.getDeclaredModels()
	require.Contains(t, dm, up.ID)
	assert.ElementsMatch(t, []string{"declared-model-1", "declared-model-2"}, dm[up.ID])
}

// ---------------------------------------------------------------------------
// ModelFilterMiddleware
// ---------------------------------------------------------------------------

func TestModelFilterMiddleware_NonModelsPath_PassesThrough(t *testing.T) {
	s := newModelFilterStore(t)
	mf := NewModelFilter(s)

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok")) //nolint:errcheck
	})

	handler := ModelFilterMiddleware(mf)(next)
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestModelFilterMiddleware_PostMethod_PassesThrough(t *testing.T) {
	s := newModelFilterStore(t)
	mf := NewModelFilter(s)

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := ModelFilterMiddleware(mf)(next)
	req := httptest.NewRequest(http.MethodPost, "/v1/models", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called, "POST /v1/models should pass through without filtering")
}

func TestModelFilterMiddleware_FiltersModelsResponse(t *testing.T) {
	s := newModelFilterStore(t)
	_, err := s.AddModelWhitelist("gpt-4*")
	require.NoError(t, err)
	mf := NewModelFilter(s)

	// Simulate an upstream that returns multiple models.
	upstream := openAIModelsResponse{
		Object: "list",
		Data: []map[string]interface{}{
			{"id": "gpt-4", "object": "model"},
			{"id": "gpt-4-turbo", "object": "model"},
			{"id": "gpt-3.5-turbo", "object": "model"},
			{"id": "claude-3-opus", "object": "model"},
		},
	}
	upstreamBody, _ := json.Marshal(upstream)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(upstreamBody) //nolint:errcheck
	})

	handler := ModelFilterMiddleware(mf)(next)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp openAIModelsResponse
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	// Only gpt-4 and gpt-4-turbo should survive the whitelist.
	ids := make([]string, 0, len(resp.Data))
	for _, m := range resp.Data {
		if id, ok := m["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	assert.ElementsMatch(t, []string{"gpt-4", "gpt-4-turbo"}, ids)
}

func TestModelFilterMiddleware_EmptyWhitelist_PassesAll(t *testing.T) {
	s := newModelFilterStore(t)
	mf := NewModelFilter(s) // no whitelist entries

	upstream := openAIModelsResponse{
		Object: "list",
		Data: []map[string]interface{}{
			{"id": "gpt-4", "object": "model"},
			{"id": "claude-3", "object": "model"},
		},
	}
	upstreamBody, _ := json.Marshal(upstream)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(upstreamBody) //nolint:errcheck
	})

	handler := ModelFilterMiddleware(mf)(next)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp openAIModelsResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Len(t, resp.Data, 2, "empty whitelist should pass all models")
}

func TestModelFilterMiddleware_UpstreamNon200_Replays(t *testing.T) {
	s := newModelFilterStore(t)
	mf := NewModelFilter(s)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "test")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`)) //nolint:errcheck
	})

	handler := ModelFilterMiddleware(mf)(next)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// 403 should be passed through unchanged (not synthesizable).
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "test", rec.Header().Get("X-Custom"))
}

func TestModelFilterMiddleware_Upstream404WithDeclaredModels_Synthesizes(t *testing.T) {
	s := newModelFilterStore(t)

	// Create upstream and set declared models.
	up, err := s.CreateUpstream("synth-up", "https://api.example.com", []string{"sk-test"}, 0, "", "", "", "", false, false)
	require.NoError(t, err)
	err = s.SetUpstreamDeclaredModels(up.ID, []string{"custom-model-1"})
	require.NoError(t, err)

	mf := NewModelFilter(s)

	// Upstream returns 404 (doesn't support /v1/models).
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found")) //nolint:errcheck
	})

	handler := ModelFilterMiddleware(mf)(next)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should synthesize a 200 response with declared models.
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp openAIModelsResponse
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "list", resp.Object)
	assert.GreaterOrEqual(t, len(resp.Data), 1)

	found := false
	for _, m := range resp.Data {
		if id, ok := m["id"].(string); ok && id == "custom-model-1" {
			found = true
		}
	}
	assert.True(t, found, "synthesized response should contain declared model")
}

func TestModelFilterMiddleware_TrailingSlash(t *testing.T) {
	s := newModelFilterStore(t)
	mf := NewModelFilter(s)

	upstream := openAIModelsResponse{
		Object: "list",
		Data: []map[string]interface{}{
			{"id": "gpt-4", "object": "model"},
		},
	}
	upstreamBody, _ := json.Marshal(upstream)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(upstreamBody) //nolint:errcheck
	})

	handler := ModelFilterMiddleware(mf)(next)
	req := httptest.NewRequest(http.MethodGet, "/v1/models/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp openAIModelsResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Len(t, resp.Data, 1)
}

// ---------------------------------------------------------------------------
// Helper: isModelsPath
// ---------------------------------------------------------------------------

func TestIsModelsPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/v1/models", true},
		{"/v1/models/", true},
		{"/v1/models/gpt-4", false},
		{"/v1/chat/completions", false},
		{"/v2/models", false},
		{"/v1/model", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, isModelsPath(tt.path))
		})
	}
}

// ---------------------------------------------------------------------------
// Helper: canSynthesizeResponse
// ---------------------------------------------------------------------------

func TestCanSynthesizeResponse(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{http.StatusNotFound, true},
		{http.StatusBadGateway, true},
		{http.StatusNotImplemented, true},
		{http.StatusForbidden, false},
		{http.StatusTooManyRequests, false},
		{http.StatusServiceUnavailable, false},
		{http.StatusOK, false},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.code), func(t *testing.T) {
			assert.Equal(t, tt.want, canSynthesizeResponse(tt.code))
		})
	}
}
