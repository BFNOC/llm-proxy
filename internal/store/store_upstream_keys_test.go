package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ResetKeyFailures
// ---------------------------------------------------------------------------

func TestResetKeyFailures_AfterIncrement(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	keys, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	rowID := keys[0].RowID

	// Increment failures a few times (threshold=0 means never auto-disable)
	_, err = s.IncrKeyFailures(up.ID, rowID, 0)
	require.NoError(t, err)
	_, err = s.IncrKeyFailures(up.ID, rowID, 0)
	require.NoError(t, err)

	// Verify failures are non-zero
	keysAfterIncr, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, keysAfterIncr[0].ConsecutiveFails)

	// Reset
	err = s.ResetKeyFailures(up.ID, rowID)
	require.NoError(t, err)

	keysAfterReset, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, keysAfterReset[0].ConsecutiveFails)
}

func TestResetKeyFailures_NonExistentKey(t *testing.T) {
	s := newTestStore(t)
	// Should not error even if the row doesn't exist (UPDATE affects 0 rows)
	err := s.ResetKeyFailures(9999, 9999)
	assert.NoError(t, err)
}

func TestResetKeyFailures_AlreadyZero(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	keys, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)

	// Reset when already zero should be a no-op
	err = s.ResetKeyFailures(up.ID, keys[0].RowID)
	require.NoError(t, err)

	keysAfter, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, keysAfter[0].ConsecutiveFails)
}

// ---------------------------------------------------------------------------
// AutoDisableFailingKeys
// ---------------------------------------------------------------------------

func TestAutoDisableFailingKeys_DisablesAboveThreshold(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"key-a", "key-b", "key-c"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	keys, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)
	require.Len(t, keys, 3)

	// Increment key-a failures to 3 (threshold=0 to avoid auto-disable during increment)
	for i := 0; i < 3; i++ {
		_, err = s.IncrKeyFailures(up.ID, keys[0].RowID, 0)
		require.NoError(t, err)
	}
	// Increment key-b failures to 1
	_, err = s.IncrKeyFailures(up.ID, keys[1].RowID, 0)
	require.NoError(t, err)
	// key-c stays at 0

	// Auto-disable keys with >= 2 failures
	affected, err := s.AutoDisableFailingKeys(2)
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected, "only key-a (3 failures) should be disabled")

	keysAfter, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)
	for _, k := range keysAfter {
		if k.RowID == keys[0].RowID {
			assert.False(t, k.Enabled, "key-a should be disabled")
		} else {
			assert.True(t, k.Enabled, "keys below threshold should stay enabled")
		}
	}
}

func TestAutoDisableFailingKeys_NoKeysAboveThreshold(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	keys, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)

	// Only 1 failure, threshold is 5
	_, err = s.IncrKeyFailures(up.ID, keys[0].RowID, 0)
	require.NoError(t, err)

	affected, err := s.AutoDisableFailingKeys(5)
	require.NoError(t, err)
	assert.Equal(t, int64(0), affected)
}

func TestAutoDisableFailingKeys_AlreadyDisabledNotCounted(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	keys, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)

	// Increment failures and manually disable
	for i := 0; i < 5; i++ {
		_, err = s.IncrKeyFailures(up.ID, keys[0].RowID, 0)
		require.NoError(t, err)
	}
	require.NoError(t, s.SetAPIKeyEnabled(up.ID, keys[0].RowID, false))

	// AutoDisable should not affect already-disabled keys
	affected, err := s.AutoDisableFailingKeys(2)
	require.NoError(t, err)
	assert.Equal(t, int64(0), affected)
}

// ---------------------------------------------------------------------------
// IncrKeyFailures (dedicated deeper coverage)
// ---------------------------------------------------------------------------

func TestIncrKeyFailures_ReturnsNewCount(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	keys, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	rowID := keys[0].RowID

	count, err := s.IncrKeyFailures(up.ID, rowID, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	count, err = s.IncrKeyFailures(up.ID, rowID, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	count, err = s.IncrKeyFailures(up.ID, rowID, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestIncrKeyFailures_AutoDisableAtThreshold(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	keys, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)
	rowID := keys[0].RowID

	// threshold=3: first two increments should not disable
	count, err := s.IncrKeyFailures(up.ID, rowID, 3)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	keysAfter, _ := s.GetUpstreamAllAPIKeys(up.ID)
	assert.True(t, keysAfter[0].Enabled, "should still be enabled below threshold")

	count, err = s.IncrKeyFailures(up.ID, rowID, 3)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	keysAfter, _ = s.GetUpstreamAllAPIKeys(up.ID)
	assert.True(t, keysAfter[0].Enabled, "should still be enabled below threshold")

	// Third increment reaches threshold=3, should auto-disable
	count, err = s.IncrKeyFailures(up.ID, rowID, 3)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
	keysAfter, _ = s.GetUpstreamAllAPIKeys(up.ID)
	assert.False(t, keysAfter[0].Enabled, "should be disabled at threshold")
}

func TestIncrKeyFailures_NonExistentKey(t *testing.T) {
	s := newTestStore(t)
	// IncrKeyFailures on non-existent row: the UPDATE affects 0 rows, but the
	// subsequent SELECT should fail because the row doesn't exist.
	_, err := s.IncrKeyFailures(9999, 9999, 0)
	require.Error(t, err, "should error when key row does not exist")
}

// ---------------------------------------------------------------------------
// GetAllUpstreamAPIKeyRowIDs
// ---------------------------------------------------------------------------

func TestGetAllUpstreamAPIKeyRowIDs_Empty(t *testing.T) {
	s := newTestStore(t)
	result, err := s.GetAllUpstreamAPIKeyRowIDs()
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGetAllUpstreamAPIKeyRowIDs_MultipleUpstreams(t *testing.T) {
	s := newTestStore(t)
	u1, err := s.CreateUpstream("up1", "https://a.example.com", []string{"k1", "k2"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("up2", "https://b.example.com", []string{"k3"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	result, err := s.GetAllUpstreamAPIKeyRowIDs()
	require.NoError(t, err)
	assert.Len(t, result[u1.ID], 2, "upstream 1 should have 2 key row IDs")
	assert.Len(t, result[u2.ID], 1, "upstream 2 should have 1 key row ID")

	// Row IDs should be positive integers
	for _, rowID := range result[u1.ID] {
		assert.Positive(t, rowID)
	}
	assert.Positive(t, result[u2.ID][0])
}

func TestGetAllUpstreamAPIKeyRowIDs_ExcludesDisabled(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"k1", "k2"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	keys, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)
	require.Len(t, keys, 2)

	// Disable the first key
	require.NoError(t, s.SetAPIKeyEnabled(up.ID, keys[0].RowID, false))

	result, err := s.GetAllUpstreamAPIKeyRowIDs()
	require.NoError(t, err)
	assert.Len(t, result[up.ID], 1, "disabled key should be excluded")
	assert.Equal(t, keys[1].RowID, result[up.ID][0])
}

// ---------------------------------------------------------------------------
// AddUpstreamAPIKeys — edge cases
// ---------------------------------------------------------------------------

func TestAddUpstreamAPIKeys_EmptyKeyList(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"existing-key"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	keys, err := s.AddUpstreamAPIKeys(up.ID, []string{})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, "existing-key", keys[0].Key)
}

func TestAddUpstreamAPIKeys_AddToUpstreamWithExistingKeys(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k1", "k2"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	keys, err := s.AddUpstreamAPIKeys(up.ID, []string{"k3", "k4"})
	require.NoError(t, err)
	require.Len(t, keys, 4)

	plainKeys := make([]string, len(keys))
	for i, k := range keys {
		plainKeys[i] = k.Key
	}
	assert.Equal(t, []string{"k1", "k2", "k3", "k4"}, plainKeys)
}

// ---------------------------------------------------------------------------
// SetAPIKeyEnabled — edge cases
// ---------------------------------------------------------------------------

func TestSetAPIKeyEnabled_EnableDisable(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	keys, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.True(t, keys[0].Enabled)

	// Disable
	err = s.SetAPIKeyEnabled(up.ID, keys[0].RowID, false)
	require.NoError(t, err)

	keysAfter, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)
	assert.False(t, keysAfter[0].Enabled)

	// Enable (should also reset consecutive_failures)
	// First add some failures
	_, err = s.IncrKeyFailures(up.ID, keys[0].RowID, 0)
	require.NoError(t, err)
	_, err = s.IncrKeyFailures(up.ID, keys[0].RowID, 0)
	require.NoError(t, err)

	err = s.SetAPIKeyEnabled(up.ID, keys[0].RowID, true)
	require.NoError(t, err)

	keysAfter, err = s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)
	assert.True(t, keysAfter[0].Enabled)
	assert.Equal(t, 0, keysAfter[0].ConsecutiveFails, "enabling should reset consecutive failures")
}

func TestSetAPIKeyEnabled_NonExistentKey(t *testing.T) {
	s := newTestStore(t)

	err := s.SetAPIKeyEnabled(9999, 9999, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------------------------------------------------------------------------
// DeleteUpstreamAPIKey — edge cases
// ---------------------------------------------------------------------------

func TestDeleteUpstreamAPIKey_NonExistentKey(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	err = s.DeleteUpstreamAPIKey(up.ID, 9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDeleteUpstreamAPIKey_WrongUpstreamID(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k"}, 0, "", "", "", "", false, false, 0)
	require.NoError(t, err)

	keys, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)

	// Try to delete with wrong upstream ID
	err = s.DeleteUpstreamAPIKey(9999, keys[0].RowID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
