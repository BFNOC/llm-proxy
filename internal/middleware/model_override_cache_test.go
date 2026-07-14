package middleware

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var overrideTestKey = []byte("01234567890123456789012345678901")

func newOverrideTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "override_test.db")
	s, err := store.NewStore(dbPath, overrideTestKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// ---------------------------------------------------------------------------
// NewModelOverrideCache
// ---------------------------------------------------------------------------

func TestNewModelOverrideCache_EmptyDB(t *testing.T) {
	s := newOverrideTestStore(t)
	oc := NewModelOverrideCache(s)
	require.NotNil(t, oc)
	// No overrides in DB — Get for any key returns nil.
	assert.Nil(t, oc.Get(999))
}

func TestNewModelOverrideCache_LoadsExistingOverrides(t *testing.T) {
	s := newOverrideTestStore(t)
	_, dk, err := s.CreateKey("oc-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false)
	require.NoError(t, err)

	require.NoError(t, s.SetKeyModelOverrides(dk.ID, []store.KeyModelOverrideInput{
		{ModelPattern: "gpt-4*", UpstreamID: u1.ID},
	}))

	oc := NewModelOverrideCache(s)
	rules := oc.Get(dk.ID)
	require.Len(t, rules, 1)
	assert.Equal(t, "gpt-4*", rules[0].ModelPattern)
	assert.Equal(t, []int64{u1.ID}, rules[0].UpstreamIDs)
}

// ---------------------------------------------------------------------------
// Reload
// ---------------------------------------------------------------------------

func TestModelOverrideCache_Reload_ReflectsChanges(t *testing.T) {
	s := newOverrideTestStore(t)
	_, dk, err := s.CreateKey("reload-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false)
	require.NoError(t, err)

	oc := NewModelOverrideCache(s)
	assert.Nil(t, oc.Get(dk.ID), "no overrides initially")

	// Add override and reload.
	require.NoError(t, s.SetKeyModelOverrides(dk.ID, []store.KeyModelOverrideInput{
		{ModelPattern: "claude-*", UpstreamID: u1.ID},
	}))
	oc.Reload()

	rules := oc.Get(dk.ID)
	require.Len(t, rules, 1)
	assert.Equal(t, "claude-*", rules[0].ModelPattern)
}

func TestModelOverrideCache_Reload_KeepsOldSnapshotOnError(t *testing.T) {
	s := newOverrideTestStore(t)
	_, dk, err := s.CreateKey("keep-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false)
	require.NoError(t, err)

	require.NoError(t, s.SetKeyModelOverrides(dk.ID, []store.KeyModelOverrideInput{
		{ModelPattern: "gpt-*", UpstreamID: u1.ID},
	}))

	oc := NewModelOverrideCache(s)
	require.Len(t, oc.Get(dk.ID), 1)

	// Close DB to force reload failure.
	_ = s.Close()
	oc.Reload() // should log error and keep old snapshot

	// Old data still available.
	rules := oc.Get(dk.ID)
	require.Len(t, rules, 1)
	assert.Equal(t, "gpt-*", rules[0].ModelPattern)
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

func TestModelOverrideCache_Get_UnknownKey(t *testing.T) {
	s := newOverrideTestStore(t)
	oc := NewModelOverrideCache(s)
	assert.Nil(t, oc.Get(12345), "unknown key should return nil")
}

func TestModelOverrideCache_Get_BeforeAnyLoad(t *testing.T) {
	// Construct cache without calling Reload (data is nil atomic.Value).
	oc := &ModelOverrideCache{store: nil}
	assert.Nil(t, oc.Get(1), "nil atomic value should return nil")
}

// ---------------------------------------------------------------------------
// convertToRules
// ---------------------------------------------------------------------------

func TestConvertToRules_Empty(t *testing.T) {
	rules := convertToRules(nil)
	assert.Empty(t, rules)
}

func TestConvertToRules_SinglePattern(t *testing.T) {
	overrides := []store.KeyModelOverride{
		{DownstreamKeyID: 1, ModelPattern: "gpt-4", UpstreamID: 10},
	}
	rules := convertToRules(overrides)
	require.Len(t, rules, 1)
	assert.Equal(t, "gpt-4", rules[0].ModelPattern)
	assert.Equal(t, []int64{10}, rules[0].UpstreamIDs)
}

func TestConvertToRules_SamePatternMergedIntoOneRule(t *testing.T) {
	overrides := []store.KeyModelOverride{
		{DownstreamKeyID: 1, ModelPattern: "claude-*", UpstreamID: 10},
		{DownstreamKeyID: 1, ModelPattern: "claude-*", UpstreamID: 20},
		{DownstreamKeyID: 1, ModelPattern: "claude-*", UpstreamID: 30},
	}
	rules := convertToRules(overrides)
	require.Len(t, rules, 1, "same pattern should be merged")
	assert.Equal(t, "claude-*", rules[0].ModelPattern)
	assert.Equal(t, []int64{10, 20, 30}, rules[0].UpstreamIDs)
}

func TestConvertToRules_MultiplePatterns_OrderPreserved(t *testing.T) {
	overrides := []store.KeyModelOverride{
		{DownstreamKeyID: 1, ModelPattern: "gpt-*", UpstreamID: 1},
		{DownstreamKeyID: 1, ModelPattern: "claude-*", UpstreamID: 2},
		{DownstreamKeyID: 1, ModelPattern: "gpt-*", UpstreamID: 3},
	}
	rules := convertToRules(overrides)
	require.Len(t, rules, 2)
	// First pattern encountered: gpt-*
	assert.Equal(t, "gpt-*", rules[0].ModelPattern)
	assert.Equal(t, []int64{1, 3}, rules[0].UpstreamIDs)
	// Second pattern: claude-*
	assert.Equal(t, "claude-*", rules[1].ModelPattern)
	assert.Equal(t, []int64{2}, rules[1].UpstreamIDs)
}

func TestConvertToRules_InvalidGlobPattern_Skipped(t *testing.T) {
	overrides := []store.KeyModelOverride{
		{DownstreamKeyID: 1, ModelPattern: "valid-*", UpstreamID: 1},
		{DownstreamKeyID: 1, ModelPattern: "[invalid*", UpstreamID: 2}, // bad glob: unclosed bracket with wildcard
		{DownstreamKeyID: 1, ModelPattern: "also-ok", UpstreamID: 3},
	}
	rules := convertToRules(overrides)
	require.Len(t, rules, 2, "invalid pattern should be skipped")
	assert.Equal(t, "valid-*", rules[0].ModelPattern)
	assert.Equal(t, "also-ok", rules[1].ModelPattern)
}

func TestConvertToRules_ExactMatch_NotValidated(t *testing.T) {
	// Patterns without * or ? are exact matches and skip glob validation.
	overrides := []store.KeyModelOverride{
		{DownstreamKeyID: 1, ModelPattern: "gpt-4o", UpstreamID: 1},
	}
	rules := convertToRules(overrides)
	require.Len(t, rules, 1)
	assert.Equal(t, "gpt-4o", rules[0].ModelPattern)
}

func TestConvertToRules_QuestionMarkGlob(t *testing.T) {
	overrides := []store.KeyModelOverride{
		{DownstreamKeyID: 1, ModelPattern: "gpt-?", UpstreamID: 5},
	}
	rules := convertToRules(overrides)
	require.Len(t, rules, 1)
	assert.Equal(t, "gpt-?", rules[0].ModelPattern)
	assert.Equal(t, []int64{5}, rules[0].UpstreamIDs)
}

// ---------------------------------------------------------------------------
// Integration: UpstreamBindingMiddleware with ModelOverrideCache
// ---------------------------------------------------------------------------

func TestBindingMW_WithModelOverrides_SetsContext(t *testing.T) {
	s := newOverrideTestStore(t)
	_, dk, err := s.CreateKey("override-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false)
	require.NoError(t, err)

	require.NoError(t, s.SetKeyModelOverrides(dk.ID, []store.KeyModelOverrideInput{
		{ModelPattern: "gpt-*", UpstreamID: u1.ID},
	}))

	bc := NewBindingCache(s)
	oc := NewModelOverrideCache(s)

	var capturedOverrides []proxy.KeyModelOverrideRule
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedOverrides = proxy.KeyModelOverridesFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mw := UpstreamBindingMiddleware(bc, oc)(next)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = withKeyID(req, dk.ID)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	require.Len(t, capturedOverrides, 1)
	assert.Equal(t, "gpt-*", capturedOverrides[0].ModelPattern)
	assert.Equal(t, []int64{u1.ID}, capturedOverrides[0].UpstreamIDs)
}
