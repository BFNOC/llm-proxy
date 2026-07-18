package store

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Downstream Key CRUD
// ---------------------------------------------------------------------------

func TestKey_Create(t *testing.T) {
	s := newTestStore(t)

	plaintext, dk, err := s.CreateKey("my-key", 60)
	require.NoError(t, err)
	require.NotNil(t, dk)

	// "sk-" prefix + 64 hex chars (32 bytes) = 67 total
	assert.True(t, strings.HasPrefix(plaintext, "sk-"), "key must start with sk-")
	assert.Equal(t, 67, len(plaintext), "key must be 67 chars total")

	assert.Positive(t, dk.ID)
	assert.Equal(t, "my-key", dk.Name)
	assert.Equal(t, 60, dk.RPMLimit)
	assert.True(t, dk.Enabled)
	assert.NotEmpty(t, dk.KeyHash)
	assert.NotEmpty(t, dk.KeyPrefix)
}

func TestKey_PlaintextNeverInDB(t *testing.T) {
	s := newTestStore(t)

	plaintext, dk, err := s.CreateKey("secure-key", 0)
	require.NoError(t, err)

	var storedHash string
	err = s.db.QueryRow(`SELECT key_hash FROM downstream_keys WHERE id = ?`, dk.ID).Scan(&storedHash)
	require.NoError(t, err)

	assert.NotEqual(t, plaintext, storedHash, "plaintext must never be stored")
	// Verify the hash matches SHA-256 of the plaintext
	h := sha256.Sum256([]byte(plaintext))
	expected := hex.EncodeToString(h[:])
	assert.Equal(t, expected, storedHash)
}

func TestKey_LookupByHash(t *testing.T) {
	s := newTestStore(t)

	plaintext, dk, err := s.CreateKey("lookup-key", 30)
	require.NoError(t, err)

	h := sha256.Sum256([]byte(plaintext))
	hash := hex.EncodeToString(h[:])

	found, err := s.LookupKeyByHash(hash)
	require.NoError(t, err)
	assert.Equal(t, dk.ID, found.ID)
	assert.Equal(t, dk.Name, found.Name)
}

func TestKey_LookupByHash_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.LookupKeyByHash("nonexistenthash")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestKey_List(t *testing.T) {
	s := newTestStore(t)

	_, _, err := s.CreateKey("key-one", 10)
	require.NoError(t, err)
	_, _, err = s.CreateKey("key-two", 20)
	require.NoError(t, err)

	keys, err := s.ListKeys()
	require.NoError(t, err)
	assert.Len(t, keys, 2)
}

func TestKey_Update(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("original", 10)
	require.NoError(t, err)

	updated, err := s.UpdateKey(dk.ID, "renamed", 99, false, nil)
	require.NoError(t, err)
	assert.Equal(t, dk.ID, updated.ID)
	assert.Equal(t, "renamed", updated.Name)
	assert.Equal(t, 99, updated.RPMLimit)
	assert.False(t, updated.Enabled)
}

func TestKey_UpdateNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.UpdateKey(9999, "name", 0, true, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestKey_Delete(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("to-delete", 0)
	require.NoError(t, err)

	err = s.DeleteKey(dk.ID)
	require.NoError(t, err)

	keys, err := s.ListKeys()
	require.NoError(t, err)
	for _, k := range keys {
		assert.NotEqual(t, dk.ID, k.ID)
	}
}

func TestKey_DeleteNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.DeleteKey(9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------------------------------------------------------------------------
// CountKeys
// ---------------------------------------------------------------------------

func TestCountKeys_Empty(t *testing.T) {
	s := newTestStore(t)
	count, err := s.CountKeys()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCountKeys_AfterCreateAndDelete(t *testing.T) {
	s := newTestStore(t)
	_, dk1, _ := s.CreateKey("k1", 0)
	s.CreateKey("k2", 0)

	count, err := s.CountKeys()
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	require.NoError(t, s.DeleteKey(dk1.ID))
	count, err = s.CountKeys()
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestCountKeys_IncludesDisabledKeys(t *testing.T) {
	s := newTestStore(t)
	_, dk, _ := s.CreateKey("disabled-key", 0)
	// Disable the key
	_, err := s.UpdateKey(dk.ID, "disabled-key", 0, false, nil)
	require.NoError(t, err)

	count, err := s.CountKeys()
	require.NoError(t, err)
	assert.Equal(t, 1, count, "disabled keys should still be counted")
}

// ---------------------------------------------------------------------------
// GetKeyPlaintext
// ---------------------------------------------------------------------------

func TestGetKeyPlaintext_Success(t *testing.T) {
	s := newTestStore(t)
	plaintext, dk, err := s.CreateKey("test-key", 0)
	require.NoError(t, err)

	recovered, err := s.GetKeyPlaintext(dk.ID)
	require.NoError(t, err)
	assert.Equal(t, plaintext, recovered, "should recover the original plaintext key")
}

func TestGetKeyPlaintext_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetKeyPlaintext(9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetKeyPlaintext_OldKeyReturnsEmpty(t *testing.T) {
	s := newTestStore(t)

	// Simulate an old key (pre-v12 migration) that has no encrypted field
	now := time.Now().UTC()
	h := sha256.Sum256([]byte("sk-oldkey"))
	keyHash := hex.EncodeToString(h[:])
	_, err := s.db.Exec(
		`INSERT INTO downstream_keys (key_hash, key_prefix, name, rpm_limit, enabled, key_encrypted, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 1, '', ?, ?)`,
		keyHash, "sk-oldke", "old-key", 0, now, now,
	)
	require.NoError(t, err)

	var id int64
	err = s.db.QueryRow(`SELECT id FROM downstream_keys WHERE key_hash = ?`, keyHash).Scan(&id)
	require.NoError(t, err)

	plain, err := s.GetKeyPlaintext(id)
	require.NoError(t, err)
	assert.Equal(t, "", plain, "old key without encrypted field should return empty string")
}

// ---------------------------------------------------------------------------
// LookupKeyByID
// ---------------------------------------------------------------------------

func TestLookupKeyByID_Success(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("lookup-id-key", 42)
	require.NoError(t, err)

	found, err := s.LookupKeyByID(dk.ID)
	require.NoError(t, err)
	assert.Equal(t, dk.ID, found.ID)
	assert.Equal(t, "lookup-id-key", found.Name)
	assert.Equal(t, 42, found.RPMLimit)
	assert.True(t, found.Enabled)
	assert.Equal(t, dk.KeyHash, found.KeyHash)
	assert.Equal(t, dk.KeyPrefix, found.KeyPrefix)
}

func TestLookupKeyByID_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.LookupKeyByID(9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestLookupKeyByID_DisabledKey(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("disabled-key", 0)
	require.NoError(t, err)

	_, err = s.UpdateKey(dk.ID, "disabled-key", 0, false, nil)
	require.NoError(t, err)

	found, err := s.LookupKeyByID(dk.ID)
	require.NoError(t, err)
	assert.Equal(t, dk.ID, found.ID)
	assert.False(t, found.Enabled, "should return the key even when disabled")
}

// ---------------------------------------------------------------------------
// GetAllKeys (0% coverage)
// ---------------------------------------------------------------------------

func TestGetAllKeys_Empty(t *testing.T) {
	s := newTestStore(t)

	keys, err := s.GetAllKeys()
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestGetAllKeys_AfterCreatingKeys(t *testing.T) {
	s := newTestStore(t)

	_, _, err := s.CreateKey("key-1", 10)
	require.NoError(t, err)
	_, _, err = s.CreateKey("key-2", 20)
	require.NoError(t, err)
	_, _, err = s.CreateKey("key-3", 30)
	require.NoError(t, err)

	keys, err := s.GetAllKeys()
	require.NoError(t, err)
	assert.Len(t, keys, 3)

	// GetAllKeys delegates to ListKeys which orders by created_at DESC
	names := make([]string, len(keys))
	for i, k := range keys {
		names[i] = k.Name
	}
	assert.Contains(t, names, "key-1")
	assert.Contains(t, names, "key-2")
	assert.Contains(t, names, "key-3")
}

// ---------------------------------------------------------------------------
// Key CRUD with MaxConcurrent（下游 Key 并发限制）
// ---------------------------------------------------------------------------

func TestCreateKey_MaxConcurrent(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("mc-key", 10)
	require.NoError(t, err)
	assert.Equal(t, 0, dk.MaxConcurrent, "默认 max_concurrent 应为 0")

	// 通过 LookupKeyByID 也应该返回 0
	found, err := s.LookupKeyByID(dk.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, found.MaxConcurrent)
}

func TestUpdateKey_MaxConcurrent(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("mc-update-key", 10)
	require.NoError(t, err)
	assert.Equal(t, 0, dk.MaxConcurrent)

	// 更新 maxConcurrent 为 50
	mc := 50
	updated, err := s.UpdateKey(dk.ID, "mc-update-key", 10, true, &mc)
	require.NoError(t, err)
	assert.Equal(t, 50, updated.MaxConcurrent)

	// 持久化验证
	found, err := s.LookupKeyByID(dk.ID)
	require.NoError(t, err)
	assert.Equal(t, 50, found.MaxConcurrent)
}

func TestUpdateKey_MaxConcurrent_Nil(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("mc-nil-key", 10)
	require.NoError(t, err)

	// 先设置 maxConcurrent 为 20
	mc := 20
	_, err = s.UpdateKey(dk.ID, "mc-nil-key", 10, true, &mc)
	require.NoError(t, err)

	// 传 nil 不应修改 maxConcurrent
	updated, err := s.UpdateKey(dk.ID, "mc-nil-key", 10, true, nil)
	require.NoError(t, err)
	assert.Equal(t, 20, updated.MaxConcurrent, "nil maxConcurrent 不应改变已有值")
}
