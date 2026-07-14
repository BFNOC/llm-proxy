package proxy

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// nextAPIKeyRoundRobin / nextAPIKeyFill / MarkKeyFailed
// ---------------------------------------------------------------------------

func TestActiveUpstream_NextAPIKey_RoundRobin(t *testing.T) {
	u := &ActiveUpstream{
		APIKeys:           []string{"k0", "k1", "k2"},
		KeyRowIDs:         []int64{10, 20, 30},
		KeySchedulingMode: "round-robin",
	}

	// First full cycle.
	for cycle := 0; cycle < 2; cycle++ {
		for i, wantKey := range []string{"k0", "k1", "k2"} {
			key, idx, rowID := u.NextAPIKey()
			assert.Equal(t, wantKey, key, "cycle=%d i=%d key", cycle, i)
			assert.Equal(t, i, idx, "cycle=%d i=%d idx", cycle, i)
			assert.Equal(t, u.KeyRowIDs[i], rowID, "cycle=%d i=%d rowID", cycle, i)
		}
	}
}

func TestActiveUpstream_NextAPIKey_SingleKey(t *testing.T) {
	u := &ActiveUpstream{
		APIKeys:   []string{"only"},
		KeyRowIDs: []int64{99},
	}
	key, idx, rowID := u.NextAPIKey()
	assert.Equal(t, "only", key)
	assert.Equal(t, 0, idx)
	assert.Equal(t, int64(99), rowID)
}

func TestActiveUpstream_NextAPIKey_NoKeys(t *testing.T) {
	u := &ActiveUpstream{}
	key, idx, rowID := u.NextAPIKey()
	assert.Equal(t, "", key)
	assert.Equal(t, -1, idx)
	assert.Equal(t, int64(-1), rowID)
}

func TestActiveUpstream_NextAPIKey_Fill_SticksThenSwitches(t *testing.T) {
	u := &ActiveUpstream{
		APIKeys:           []string{"a", "b", "c"},
		KeyRowIDs:         []int64{1, 2, 3},
		KeySchedulingMode: "fill",
	}

	// Fill mode sticks to the first key.
	for i := 0; i < 5; i++ {
		key, idx, _ := u.NextAPIKey()
		assert.Equal(t, "a", key, "should stick to first key, call %d", i)
		assert.Equal(t, 0, idx)
	}

	// Mark failed -> switches to next.
	u.MarkKeyFailed()
	key, idx, rowID := u.NextAPIKey()
	assert.Equal(t, "b", key)
	assert.Equal(t, 1, idx)
	assert.Equal(t, int64(2), rowID)

	// Sticks to "b" now.
	key2, _, _ := u.NextAPIKey()
	assert.Equal(t, "b", key2)

	// Fail again -> switches to "c".
	u.MarkKeyFailed()
	key3, idx3, _ := u.NextAPIKey()
	assert.Equal(t, "c", key3)
	assert.Equal(t, 2, idx3)

	// Fail again -> wraps around to "a".
	u.MarkKeyFailed()
	key4, idx4, _ := u.NextAPIKey()
	assert.Equal(t, "a", key4)
	assert.Equal(t, 0, idx4)
}

func TestActiveUpstream_NextAPIKey_RoundRobin_MissingRowIDs(t *testing.T) {
	// KeyRowIDs shorter than APIKeys — should return -1 for missing entries.
	u := &ActiveUpstream{
		APIKeys:           []string{"k0", "k1"},
		KeyRowIDs:         []int64{10},
		KeySchedulingMode: "round-robin",
	}
	_, _, rowID0 := u.NextAPIKey()
	assert.Equal(t, int64(10), rowID0)

	_, _, rowID1 := u.NextAPIKey()
	assert.Equal(t, int64(-1), rowID1, "missing rowID should be -1")
}

// ---------------------------------------------------------------------------
// filterUpstreamsByModel
// ---------------------------------------------------------------------------

func TestFilterUpstreamsByModel(t *testing.T) {
	tests := []struct {
		name      string
		upstreams []*ActiveUpstream
		model     string
		wantNames []string
	}{
		{
			name: "empty patterns accepts all models",
			upstreams: []*ActiveUpstream{
				{Name: "all", ModelPatterns: nil},
			},
			model:     "gpt-4o",
			wantNames: []string{"all"},
		},
		{
			name: "glob match",
			upstreams: []*ActiveUpstream{
				{Name: "gpt", ModelPatterns: []string{"gpt-4*"}},
			},
			model:     "gpt-4o",
			wantNames: []string{"gpt"},
		},
		{
			name: "no match excluded",
			upstreams: []*ActiveUpstream{
				{Name: "claude", ModelPatterns: []string{"claude-*"}},
			},
			model:     "gpt-4o",
			wantNames: nil,
		},
		{
			name: "multiple upstreams mixed",
			upstreams: []*ActiveUpstream{
				{Name: "openai", ModelPatterns: []string{"gpt-*"}},
				{Name: "anthropic", ModelPatterns: []string{"claude-*"}},
				{Name: "wildcard", ModelPatterns: nil},
			},
			model:     "claude-3-opus",
			wantNames: []string{"anthropic", "wildcard"},
		},
		{
			name: "multiple patterns on one upstream",
			upstreams: []*ActiveUpstream{
				{Name: "multi", ModelPatterns: []string{"gpt-*", "claude-*"}},
			},
			model:     "claude-3-opus",
			wantNames: []string{"multi"},
		},
		{
			name:      "empty upstream list",
			upstreams: nil,
			model:     "gpt-4",
			wantNames: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filterUpstreamsByModel(tc.upstreams, tc.model)
			var gotNames []string
			for _, u := range got {
				gotNames = append(gotNames, u.Name)
			}
			assert.Equal(t, tc.wantNames, gotNames)
		})
	}
}

// ---------------------------------------------------------------------------
// extractModelFromBody
// ---------------------------------------------------------------------------

func TestExtractModelFromBody(t *testing.T) {
	tests := []struct {
		name      string
		body      []byte
		wantModel string
		wantIsJSON bool
	}{
		{
			name:       "valid JSON with model",
			body:       []byte(`{"model": "gpt-4", "messages": []}`),
			wantModel:  "gpt-4",
			wantIsJSON: true,
		},
		{
			name:       "no model field",
			body:       []byte(`{"messages": []}`),
			wantModel:  "",
			wantIsJSON: true,
		},
		{
			name:       "invalid JSON",
			body:       []byte(`not json at all`),
			wantModel:  "",
			wantIsJSON: false,
		},
		{
			name:       "empty body",
			body:       []byte{},
			wantModel:  "",
			wantIsJSON: false,
		},
		{
			name:       "nil body",
			body:       nil,
			wantModel:  "",
			wantIsJSON: false,
		},
		{
			name:       "model is null",
			body:       []byte(`{"model": null}`),
			wantModel:  "",
			wantIsJSON: true,
		},
		{
			name:       "model is number",
			body:       []byte(`{"model": 42}`),
			wantModel:  "",
			wantIsJSON: true,
		},
		{
			name:       "model nested not at top level",
			body:       []byte(`{"config": {"model": "gpt-4"}}`),
			wantModel:  "",
			wantIsJSON: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			model, isJSON := extractModelFromBody(tc.body)
			assert.Equal(t, tc.wantModel, model)
			assert.Equal(t, tc.wantIsJSON, isJSON)
		})
	}
}

// ---------------------------------------------------------------------------
// Context round-trip: AllowedUpstreamIDs
// ---------------------------------------------------------------------------

func TestContextWithAllowedUpstreamIDs_RoundTrip(t *testing.T) {
	ctx := context.Background()

	// No value set -> nil.
	assert.Nil(t, AllowedUpstreamIDsFromContext(ctx))

	ids := []int64{1, 2, 3}
	ctx = ContextWithAllowedUpstreamIDs(ctx, ids)

	got := AllowedUpstreamIDsFromContext(ctx)
	require.NotNil(t, got)
	assert.Equal(t, ids, got)
}

func TestContextWithAllowedUpstreamIDs_EmptySlice(t *testing.T) {
	ctx := ContextWithAllowedUpstreamIDs(context.Background(), []int64{})
	got := AllowedUpstreamIDsFromContext(ctx)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

// ---------------------------------------------------------------------------
// Context round-trip: KeyModelOverrides
// ---------------------------------------------------------------------------

func TestContextWithKeyModelOverrides_RoundTrip(t *testing.T) {
	ctx := context.Background()

	// No value set -> nil.
	assert.Nil(t, KeyModelOverridesFromContext(ctx))

	overrides := []KeyModelOverrideRule{
		{ModelPattern: "gpt-4*", UpstreamIDs: []int64{10, 20}},
		{ModelPattern: "claude-*", UpstreamIDs: []int64{30}},
	}
	ctx = ContextWithKeyModelOverrides(ctx, overrides)

	got := KeyModelOverridesFromContext(ctx)
	require.NotNil(t, got)
	assert.Equal(t, overrides, got)
}

func TestContextWithKeyModelOverrides_EmptySlice(t *testing.T) {
	ctx := ContextWithKeyModelOverrides(context.Background(), []KeyModelOverrideRule{})
	got := KeyModelOverridesFromContext(ctx)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

// ---------------------------------------------------------------------------
// SetAllUpstreams preserves key scheduling cursors
// ---------------------------------------------------------------------------

func TestSetAllUpstreams_PreservesKeyCursors(t *testing.T) {
	dp := NewDynamicProxy()

	u1, err := url.Parse("https://api.openai.com")
	require.NoError(t, err)

	original := &ActiveUpstream{
		ID:                1,
		BaseURL:           u1,
		APIKeys:           []string{"a", "b", "c"},
		KeyRowIDs:         []int64{10, 20, 30},
		KeySchedulingMode: "round-robin",
	}
	dp.SetAllUpstreams([]*ActiveUpstream{original})

	// Advance round-robin cursor twice (now at index 2).
	original.NextAPIKey() // a
	original.NextAPIKey() // b

	// Rebuild with a new ActiveUpstream for the same ID.
	rebuilt := &ActiveUpstream{
		ID:                1,
		BaseURL:           u1,
		APIKeys:           []string{"a", "b", "c"},
		KeyRowIDs:         []int64{10, 20, 30},
		KeySchedulingMode: "round-robin",
	}
	dp.SetAllUpstreams([]*ActiveUpstream{rebuilt})

	// The cursor should have been carried over — next key should be "c".
	key, idx, _ := rebuilt.NextAPIKey()
	assert.Equal(t, "c", key)
	assert.Equal(t, 2, idx)
}
