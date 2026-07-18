package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Key-Upstream Bindings
// ---------------------------------------------------------------------------

func TestKeyUpstreamBinding_SetAndGet(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("bound-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("upstream-1", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("upstream-2", "https://b.example.com", []string{"key-b"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetKeyUpstreams(dk.ID, []int64{u1.ID, u2.ID})
	require.NoError(t, err)

	ids, err := s.GetKeyUpstreamIDs(dk.ID)
	require.NoError(t, err)
	assert.Len(t, ids, 2)
	assert.Contains(t, ids, u1.ID)
	assert.Contains(t, ids, u2.ID)
}

func TestKeyUpstreamBinding_ReplaceExisting(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("replace-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("up-1", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("up-2", "https://b.example.com", []string{"key-b"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetKeyUpstreams(dk.ID, []int64{u1.ID})
	require.NoError(t, err)

	// Replace with a different set
	err = s.SetKeyUpstreams(dk.ID, []int64{u2.ID})
	require.NoError(t, err)

	ids, err := s.GetKeyUpstreamIDs(dk.ID)
	require.NoError(t, err)
	assert.Len(t, ids, 1)
	assert.Equal(t, u2.ID, ids[0])
}

func TestKeyUpstreamBinding_EmptyMeansAll(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("empty-key", 0)
	require.NoError(t, err)

	ids, err := s.GetKeyUpstreamIDs(dk.ID)
	require.NoError(t, err)
	assert.Empty(t, ids, "no bindings should return empty slice")
}

func TestKeyUpstreamBinding_ClearBindings(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("clear-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("up-c", "https://c.example.com", []string{"key-c"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetKeyUpstreams(dk.ID, []int64{u1.ID})
	require.NoError(t, err)

	// Clear by passing empty
	err = s.SetKeyUpstreams(dk.ID, []int64{})
	require.NoError(t, err)

	ids, err := s.GetKeyUpstreamIDs(dk.ID)
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestKeyUpstreamBinding_CascadeDeleteKey(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("cascade-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("up-d", "https://d.example.com", []string{"key-d"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetKeyUpstreams(dk.ID, []int64{u1.ID})
	require.NoError(t, err)

	// Delete the key — bindings should cascade delete
	err = s.DeleteKey(dk.ID)
	require.NoError(t, err)

	// Verify bindings are gone (query raw DB since GetKeyUpstreamIDs has no key check)
	ids, err := s.GetKeyUpstreamIDs(dk.ID)
	require.NoError(t, err)
	assert.Empty(t, ids, "bindings should be deleted when key is deleted")
}

func TestKeyUpstreamBinding_CascadeDeleteUpstream(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("cascade-up-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("up-e", "https://e.example.com", []string{"key-e"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("up-f", "https://f.example.com", []string{"key-f"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetKeyUpstreams(dk.ID, []int64{u1.ID, u2.ID})
	require.NoError(t, err)

	// Delete one upstream — that binding should cascade delete
	err = s.HardDeleteUpstream(u1.ID)
	require.NoError(t, err)

	ids, err := s.GetKeyUpstreamIDs(dk.ID)
	require.NoError(t, err)
	assert.Len(t, ids, 1)
	assert.Equal(t, u2.ID, ids[0])
}

// ---------------------------------------------------------------------------
// GetAllKeyBindings
// ---------------------------------------------------------------------------

func TestGetAllKeyBindings_Empty(t *testing.T) {
	s := newTestStore(t)
	bindings, err := s.GetAllKeyBindings()
	require.NoError(t, err)
	assert.Empty(t, bindings)
}

func TestGetAllKeyBindings_GroupedAndSorted(t *testing.T) {
	s := newTestStore(t)
	_, dk1, _ := s.CreateKey("k1", 0)
	_, dk2, _ := s.CreateKey("k2", 0)
	u1, _ := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	u2, _ := s.CreateUpstream("u2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "", false, false, 0)
	u3, _ := s.CreateUpstream("u3", "https://c.example.com", []string{"kc"}, 0, "", "", "", "", false, false, 0)

	require.NoError(t, s.SetKeyUpstreams(dk1.ID, []int64{u2.ID, u1.ID})) // insert out of order
	require.NoError(t, s.SetKeyUpstreams(dk2.ID, []int64{u3.ID}))

	bindings, err := s.GetAllKeyBindings()
	require.NoError(t, err)

	require.Len(t, bindings[dk1.ID], 2)
	assert.Equal(t, u1.ID, bindings[dk1.ID][0], "should be sorted by upstream_id")
	assert.Equal(t, u2.ID, bindings[dk1.ID][1])
	require.Len(t, bindings[dk2.ID], 1)
	assert.Equal(t, u3.ID, bindings[dk2.ID][0])
}

func TestGetAllKeyBindings_UnboundKeysAbsent(t *testing.T) {
	s := newTestStore(t)
	s.CreateKey("unbound", 0)

	bindings, err := s.GetAllKeyBindings()
	require.NoError(t, err)
	assert.Empty(t, bindings, "unbound key should not appear in map")
}

func TestGetAllKeyBindings_AfterReplaceAndClear(t *testing.T) {
	s := newTestStore(t)
	_, dk, _ := s.CreateKey("replace-key", 0)
	u1, _ := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	u2, _ := s.CreateUpstream("u2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "", false, false, 0)

	require.NoError(t, s.SetKeyUpstreams(dk.ID, []int64{u1.ID}))
	require.NoError(t, s.SetKeyUpstreams(dk.ID, []int64{u2.ID})) // Replace

	bindings, err := s.GetAllKeyBindings()
	require.NoError(t, err)
	require.Len(t, bindings[dk.ID], 1)
	assert.Equal(t, u2.ID, bindings[dk.ID][0])

	require.NoError(t, s.SetKeyUpstreams(dk.ID, []int64{})) // Clear
	bindings, err = s.GetAllKeyBindings()
	require.NoError(t, err)
	assert.Empty(t, bindings[dk.ID])
}

// ---------------------------------------------------------------------------
// Upstream Model Patterns
// ---------------------------------------------------------------------------

func TestUpstreamModelPatterns_SetAndGet(t *testing.T) {
	s := newTestStore(t)
	u, err := s.CreateUpstream("test-up", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetUpstreamModelPatterns(u.ID, []string{"claude-*", "gpt-4o"})
	require.NoError(t, err)

	patterns, err := s.GetUpstreamModelPatterns(u.ID)
	require.NoError(t, err)
	assert.Len(t, patterns, 2)
	assert.Contains(t, patterns, "claude-*")
	assert.Contains(t, patterns, "gpt-4o")
}

func TestUpstreamModelPatterns_Overwrite(t *testing.T) {
	s := newTestStore(t)
	u, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetUpstreamModelPatterns(u.ID, []string{"claude-*"})
	require.NoError(t, err)

	err = s.SetUpstreamModelPatterns(u.ID, []string{"gpt-*"})
	require.NoError(t, err)

	patterns, err := s.GetUpstreamModelPatterns(u.ID)
	require.NoError(t, err)
	assert.Len(t, patterns, 1)
	assert.Equal(t, "gpt-*", patterns[0])
}

func TestUpstreamModelPatterns_ClearPatterns(t *testing.T) {
	s := newTestStore(t)
	u, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetUpstreamModelPatterns(u.ID, []string{"claude-*"})
	require.NoError(t, err)

	err = s.SetUpstreamModelPatterns(u.ID, []string{})
	require.NoError(t, err)

	patterns, err := s.GetUpstreamModelPatterns(u.ID)
	require.NoError(t, err)
	assert.Empty(t, patterns)
}

func TestUpstreamModelPatterns_CascadeDeleteUpstream(t *testing.T) {
	s := newTestStore(t)
	u, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetUpstreamModelPatterns(u.ID, []string{"claude-*", "gpt-*"})
	require.NoError(t, err)

	err = s.HardDeleteUpstream(u.ID)
	require.NoError(t, err)

	patterns, err := s.GetUpstreamModelPatterns(u.ID)
	require.NoError(t, err)
	assert.Empty(t, patterns, "patterns should cascade delete with upstream")
}

func TestUpstreamModelPatterns_GetAll(t *testing.T) {
	s := newTestStore(t)
	u1, _ := s.CreateUpstream("up1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	u2, _ := s.CreateUpstream("up2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "", false, false, 0)

	require.NoError(t, s.SetUpstreamModelPatterns(u1.ID, []string{"claude-*"}))
	require.NoError(t, s.SetUpstreamModelPatterns(u2.ID, []string{"gpt-*", "o1-*"}))

	all, err := s.GetAllUpstreamModelPatterns()
	require.NoError(t, err)
	assert.Len(t, all[u1.ID], 1)
	assert.Equal(t, "claude-*", all[u1.ID][0])
	assert.Len(t, all[u2.ID], 2)
}

func TestUpstreamModelPatterns_EmptyByDefault(t *testing.T) {
	s := newTestStore(t)
	u, _ := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)

	patterns, err := s.GetUpstreamModelPatterns(u.ID)
	require.NoError(t, err)
	assert.Empty(t, patterns, "no patterns by default")
}

// ---------------------------------------------------------------------------
// DeleteModelWhitelist / BatchDeleteModelWhitelist
// ---------------------------------------------------------------------------

func TestDeleteModelWhitelist_Success(t *testing.T) {
	s := newTestStore(t)

	entry, err := s.AddModelWhitelist("claude-*")
	require.NoError(t, err)

	err = s.DeleteModelWhitelist(entry.ID)
	require.NoError(t, err)

	list, err := s.ListModelWhitelist()
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestDeleteModelWhitelist_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.DeleteModelWhitelist(9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestBatchDeleteModelWhitelist_Multiple(t *testing.T) {
	s := newTestStore(t)

	e1, err := s.AddModelWhitelist("gpt-*")
	require.NoError(t, err)
	e2, err := s.AddModelWhitelist("claude-*")
	require.NoError(t, err)
	_, err = s.AddModelWhitelist("o1-*")
	require.NoError(t, err)

	deleted, err := s.BatchDeleteModelWhitelist([]int64{e1.ID, e2.ID})
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)

	list, err := s.ListModelWhitelist()
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, "o1-*", list[0].Pattern)
}

func TestBatchDeleteModelWhitelist_Empty(t *testing.T) {
	s := newTestStore(t)
	deleted, err := s.BatchDeleteModelWhitelist([]int64{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)
}

func TestBatchDeleteModelWhitelist_NonExistentIDs(t *testing.T) {
	s := newTestStore(t)
	deleted, err := s.BatchDeleteModelWhitelist([]int64{9999, 8888})
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)
}

// ---------------------------------------------------------------------------
// SetKeyModelOverrides / GetKeyModelOverrides
// ---------------------------------------------------------------------------

func TestKeyModelOverrides_SetAndGet(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("override-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("up1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("up2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	overrides := []KeyModelOverrideInput{
		{ModelPattern: "claude-*", UpstreamID: u1.ID},
		{ModelPattern: "gpt-4o", UpstreamID: u2.ID},
	}
	err = s.SetKeyModelOverrides(dk.ID, overrides)
	require.NoError(t, err)

	got, err := s.GetKeyModelOverrides(dk.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, dk.ID, got[0].DownstreamKeyID)
	assert.Equal(t, dk.ID, got[1].DownstreamKeyID)

	// Ordered by model_pattern, upstream_id
	patterns := []string{got[0].ModelPattern, got[1].ModelPattern}
	assert.Contains(t, patterns, "claude-*")
	assert.Contains(t, patterns, "gpt-4o")
}

func TestKeyModelOverrides_Overwrite(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("ow-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("u2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetKeyModelOverrides(dk.ID, []KeyModelOverrideInput{
		{ModelPattern: "old-*", UpstreamID: u1.ID},
	})
	require.NoError(t, err)

	// Overwrite with new set
	err = s.SetKeyModelOverrides(dk.ID, []KeyModelOverrideInput{
		{ModelPattern: "new-*", UpstreamID: u2.ID},
	})
	require.NoError(t, err)

	got, err := s.GetKeyModelOverrides(dk.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "new-*", got[0].ModelPattern)
	assert.Equal(t, u2.ID, got[0].UpstreamID)
}

func TestKeyModelOverrides_Clear(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("clear-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetKeyModelOverrides(dk.ID, []KeyModelOverrideInput{
		{ModelPattern: "claude-*", UpstreamID: u1.ID},
	})
	require.NoError(t, err)

	// Clear all overrides
	err = s.SetKeyModelOverrides(dk.ID, []KeyModelOverrideInput{})
	require.NoError(t, err)

	got, err := s.GetKeyModelOverrides(dk.ID)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestKeyModelOverrides_GetEmptyByDefault(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("no-override-key", 0)
	require.NoError(t, err)

	got, err := s.GetKeyModelOverrides(dk.ID)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestKeyModelOverrides_MultipleUpstreamsForSamePattern(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("multi-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("u2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	// Same model pattern, two upstreams (for failover)
	err = s.SetKeyModelOverrides(dk.ID, []KeyModelOverrideInput{
		{ModelPattern: "claude-*", UpstreamID: u1.ID},
		{ModelPattern: "claude-*", UpstreamID: u2.ID},
	})
	require.NoError(t, err)

	got, err := s.GetKeyModelOverrides(dk.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "claude-*", got[0].ModelPattern)
	assert.Equal(t, "claude-*", got[1].ModelPattern)
}

func TestGetAllKeyModelOverrides(t *testing.T) {
	s := newTestStore(t)
	_, dk1, err := s.CreateKey("k1", 0)
	require.NoError(t, err)
	_, dk2, err := s.CreateKey("k2", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	require.NoError(t, s.SetKeyModelOverrides(dk1.ID, []KeyModelOverrideInput{
		{ModelPattern: "claude-*", UpstreamID: u1.ID},
	}))
	require.NoError(t, s.SetKeyModelOverrides(dk2.ID, []KeyModelOverrideInput{
		{ModelPattern: "gpt-*", UpstreamID: u1.ID},
	}))

	all, err := s.GetAllKeyModelOverrides()
	require.NoError(t, err)
	require.Len(t, all[dk1.ID], 1)
	assert.Equal(t, "claude-*", all[dk1.ID][0].ModelPattern)
	require.Len(t, all[dk2.ID], 1)
	assert.Equal(t, "gpt-*", all[dk2.ID][0].ModelPattern)
}

func TestGetAllKeyModelOverrides_Empty(t *testing.T) {
	s := newTestStore(t)
	all, err := s.GetAllKeyModelOverrides()
	require.NoError(t, err)
	assert.Empty(t, all)
}

// ---------------------------------------------------------------------------
// Upstream Declared Models
// ---------------------------------------------------------------------------

func TestUpstreamDeclaredModels_SetAndGet(t *testing.T) {
	s := newTestStore(t)
	u, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetUpstreamDeclaredModels(u.ID, []string{"gpt-4o", "gpt-4o-mini", "claude-sonnet-4-20250514"})
	require.NoError(t, err)

	models, err := s.GetUpstreamDeclaredModels(u.ID)
	require.NoError(t, err)
	assert.Len(t, models, 3)
	// Sorted by model_id
	assert.Equal(t, "claude-sonnet-4-20250514", models[0])
	assert.Equal(t, "gpt-4o", models[1])
	assert.Equal(t, "gpt-4o-mini", models[2])
}

func TestUpstreamDeclaredModels_Overwrite(t *testing.T) {
	s := newTestStore(t)
	u, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetUpstreamDeclaredModels(u.ID, []string{"gpt-4o"})
	require.NoError(t, err)

	err = s.SetUpstreamDeclaredModels(u.ID, []string{"claude-sonnet-4-20250514", "claude-haiku-3.5"})
	require.NoError(t, err)

	models, err := s.GetUpstreamDeclaredModels(u.ID)
	require.NoError(t, err)
	assert.Len(t, models, 2)
	assert.Contains(t, models, "claude-sonnet-4-20250514")
	assert.Contains(t, models, "claude-haiku-3.5")
}

func TestUpstreamDeclaredModels_ClearModels(t *testing.T) {
	s := newTestStore(t)
	u, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetUpstreamDeclaredModels(u.ID, []string{"gpt-4o"})
	require.NoError(t, err)

	err = s.SetUpstreamDeclaredModels(u.ID, []string{})
	require.NoError(t, err)

	models, err := s.GetUpstreamDeclaredModels(u.ID)
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestUpstreamDeclaredModels_EmptyByDefault(t *testing.T) {
	s := newTestStore(t)
	u, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	models, err := s.GetUpstreamDeclaredModels(u.ID)
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestUpstreamDeclaredModels_CascadeDeleteUpstream(t *testing.T) {
	s := newTestStore(t)
	u, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetUpstreamDeclaredModels(u.ID, []string{"gpt-4o", "gpt-4o-mini"})
	require.NoError(t, err)

	err = s.HardDeleteUpstream(u.ID)
	require.NoError(t, err)

	models, err := s.GetUpstreamDeclaredModels(u.ID)
	require.NoError(t, err)
	assert.Empty(t, models, "declared models should cascade delete with upstream")
}

func TestGetAllUpstreamDeclaredModels_Empty(t *testing.T) {
	s := newTestStore(t)
	all, err := s.GetAllUpstreamDeclaredModels()
	require.NoError(t, err)
	assert.Empty(t, all)
}

func TestGetAllUpstreamDeclaredModels_MultipleUpstreams(t *testing.T) {
	s := newTestStore(t)
	u1, err := s.CreateUpstream("up1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("up2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	require.NoError(t, s.SetUpstreamDeclaredModels(u1.ID, []string{"gpt-4o"}))
	require.NoError(t, s.SetUpstreamDeclaredModels(u2.ID, []string{"claude-sonnet-4-20250514", "claude-haiku-3.5"}))

	all, err := s.GetAllUpstreamDeclaredModels()
	require.NoError(t, err)
	assert.Len(t, all[u1.ID], 1)
	assert.Equal(t, "gpt-4o", all[u1.ID][0])
	assert.Len(t, all[u2.ID], 2)
}

func TestGetAllUpstreamDeclaredModels_ExcludesDisabledUpstream(t *testing.T) {
	s := newTestStore(t)
	u1, err := s.CreateUpstream("up1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("up2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	require.NoError(t, s.SetUpstreamDeclaredModels(u1.ID, []string{"gpt-4o"}))
	require.NoError(t, s.SetUpstreamDeclaredModels(u2.ID, []string{"claude-sonnet-4-20250514"}))

	// Disable u1
	_, err = s.UpdateUpstream(u1.ID, "up1", "https://a.example.com", nil, 0, false, "", "", "", "", nil)
	require.NoError(t, err)

	all, err := s.GetAllUpstreamDeclaredModels()
	require.NoError(t, err)
	assert.Empty(t, all[u1.ID], "disabled upstream should not appear in GetAllUpstreamDeclaredModels")
	assert.Len(t, all[u2.ID], 1)
}

// ---------------------------------------------------------------------------
// SetKeyModelOverrides — deeper coverage (cascade behavior)
// ---------------------------------------------------------------------------

func TestKeyModelOverrides_CascadeDeleteKey(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("cascade-override-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetKeyModelOverrides(dk.ID, []KeyModelOverrideInput{
		{ModelPattern: "claude-*", UpstreamID: u1.ID},
	})
	require.NoError(t, err)

	// Delete the key — overrides should cascade
	err = s.DeleteKey(dk.ID)
	require.NoError(t, err)

	got, err := s.GetKeyModelOverrides(dk.ID)
	require.NoError(t, err)
	assert.Empty(t, got, "overrides should cascade delete when key is deleted")
}

func TestKeyModelOverrides_CascadeDeleteUpstream(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("cascade-up-override-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("u2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.SetKeyModelOverrides(dk.ID, []KeyModelOverrideInput{
		{ModelPattern: "claude-*", UpstreamID: u1.ID},
		{ModelPattern: "gpt-*", UpstreamID: u2.ID},
	})
	require.NoError(t, err)

	// Delete u1 — only its override should cascade
	err = s.HardDeleteUpstream(u1.ID)
	require.NoError(t, err)

	got, err := s.GetKeyModelOverrides(dk.ID)
	require.NoError(t, err)
	require.Len(t, got, 1, "only the override for u2 should remain")
	assert.Equal(t, "gpt-*", got[0].ModelPattern)
	assert.Equal(t, u2.ID, got[0].UpstreamID)
}

// ---------------------------------------------------------------------------
// SetKeyUpstreams — edge cases
// ---------------------------------------------------------------------------

func TestSetKeyUpstreams_SetMultiple(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("k", 0)
	require.NoError(t, err)
	u1, _ := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	u2, _ := s.CreateUpstream("u2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "", false, false, 0)
	u3, _ := s.CreateUpstream("u3", "https://c.example.com", []string{"kc"}, 0, "", "", "", "", false, false, 0)

	err = s.SetKeyUpstreams(dk.ID, []int64{u1.ID, u2.ID, u3.ID})
	require.NoError(t, err)

	ids, err := s.GetKeyUpstreamIDs(dk.ID)
	require.NoError(t, err)
	assert.Len(t, ids, 3)
}

func TestSetKeyUpstreams_SetForNonExistentKey(t *testing.T) {
	s := newTestStore(t)

	u1, _ := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)

	// FK constraint on downstream_key_id should cause an error
	err := s.SetKeyUpstreams(9999, []int64{u1.ID})
	require.Error(t, err, "should error for nonexistent key due to FK constraint")
}

// ---------------------------------------------------------------------------
// SetUpstreamModelPatterns — edge cases
// ---------------------------------------------------------------------------

func TestSetUpstreamModelPatterns_SetForNonExistentUpstream(t *testing.T) {
	s := newTestStore(t)

	// FK constraint on upstream_id should cause an error
	err := s.SetUpstreamModelPatterns(9999, []string{"gpt-*"})
	require.Error(t, err, "should error for nonexistent upstream due to FK constraint")
}

// ---------------------------------------------------------------------------
// SetUpstreamDeclaredModels — edge cases
// ---------------------------------------------------------------------------

func TestSetUpstreamDeclaredModels_SetForNonExistentUpstream(t *testing.T) {
	s := newTestStore(t)

	err := s.SetUpstreamDeclaredModels(9999, []string{"gpt-4o"})
	require.Error(t, err, "should error for nonexistent upstream due to FK constraint")
}

// ---------------------------------------------------------------------------
// SetKeyModelOverrides — edge cases
// ---------------------------------------------------------------------------

func TestSetKeyModelOverrides_MultipleOverridesSamePatternFailover(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("k", 0)
	require.NoError(t, err)
	u1, _ := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)
	u2, _ := s.CreateUpstream("u2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "", false, false, 0)
	u3, _ := s.CreateUpstream("u3", "https://c.example.com", []string{"kc"}, 0, "", "", "", "", false, false, 0)

	// Three upstreams for the same pattern (failover chain)
	err = s.SetKeyModelOverrides(dk.ID, []KeyModelOverrideInput{
		{ModelPattern: "gpt-4o", UpstreamID: u1.ID},
		{ModelPattern: "gpt-4o", UpstreamID: u2.ID},
		{ModelPattern: "gpt-4o", UpstreamID: u3.ID},
	})
	require.NoError(t, err)

	got, err := s.GetKeyModelOverrides(dk.ID)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	for _, o := range got {
		assert.Equal(t, "gpt-4o", o.ModelPattern)
	}
}

func TestSetKeyModelOverrides_ClearOverrides(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("k", 0)
	require.NoError(t, err)
	u1, _ := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "", false, false, 0)

	err = s.SetKeyModelOverrides(dk.ID, []KeyModelOverrideInput{
		{ModelPattern: "claude-*", UpstreamID: u1.ID},
	})
	require.NoError(t, err)

	// Clear
	err = s.SetKeyModelOverrides(dk.ID, nil)
	require.NoError(t, err)

	got, err := s.GetKeyModelOverrides(dk.ID)
	require.NoError(t, err)
	assert.Empty(t, got)
}
