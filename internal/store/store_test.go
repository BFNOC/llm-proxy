package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testKey = []byte("01234567890123456789012345678901") // exactly 32 bytes

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewStore(dbPath, testKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// ---------------------------------------------------------------------------
// NewStore
// ---------------------------------------------------------------------------

func TestNewStore_ValidKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewStore(dbPath, testKey)
	require.NoError(t, err)
	require.NotNil(t, s)
	assert.NoError(t, s.Close())
}

func TestNewStore_InvalidKey_TooShort(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	_, err := NewStore(dbPath, []byte("tooshort"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}

func TestNewStore_InvalidKey_TooLong(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	_, err := NewStore(dbPath, []byte("this-key-is-way-too-long-33bytes!"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}

func TestNewStore_InvalidKey_Empty(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	_, err := NewStore(dbPath, []byte{})
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Encryption
// ---------------------------------------------------------------------------

func TestEncrypt_RoundTrip(t *testing.T) {
	plaintext := "sk-supersecretapikey"
	ciphertext, err := Encrypt(plaintext, testKey)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext)

	decrypted, err := Decrypt(ciphertext, testKey)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncrypt_VersionPrefix(t *testing.T) {
	ciphertext, err := Encrypt("somevalue", testKey)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(ciphertext, "v1:"), "expected v1: prefix, got: %s", ciphertext)
}

func TestEncrypt_EmptyString(t *testing.T) {
	ciphertext, err := Encrypt("", testKey)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(ciphertext, "v1:"))

	decrypted, err := Decrypt(ciphertext, testKey)
	require.NoError(t, err)
	assert.Equal(t, "", decrypted)
}

func TestEncrypt_DifferentCiphertextEachCall(t *testing.T) {
	plaintext := "same-input"
	c1, err := Encrypt(plaintext, testKey)
	require.NoError(t, err)
	c2, err := Encrypt(plaintext, testKey)
	require.NoError(t, err)
	// GCM uses random nonce, so ciphertexts must differ
	assert.NotEqual(t, c1, c2)
}

func TestDecrypt_InvalidVersion(t *testing.T) {
	_, err := Decrypt("v2:someciphertext", testKey)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version")
}

func TestDecrypt_WrongKey(t *testing.T) {
	ciphertext, err := Encrypt("secret", testKey)
	require.NoError(t, err)

	wrongKey := []byte("99999999999999999999999999999999")
	_, err = Decrypt(ciphertext, wrongKey)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Migration idempotency
// ---------------------------------------------------------------------------

func TestRunMigrations_Idempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migrate.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()

	err = RunMigrations(db)
	require.NoError(t, err, "first migration run should succeed")

	err = RunMigrations(db)
	require.NoError(t, err, "second migration run should be idempotent")
}

// ---------------------------------------------------------------------------
// Upstream CRUD
// ---------------------------------------------------------------------------

func TestUpstream_CreateAndGet(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("openai", "https://api.openai.com", []string{"sk-key123"}, 10, "", "", "", "")
	require.NoError(t, err)
	require.NotNil(t, up)
	assert.Positive(t, up.ID)
	assert.Equal(t, "openai", up.Name)
	assert.Equal(t, "https://api.openai.com", up.BaseURL)
	assert.Equal(t, []string{"sk-key123"}, up.APIKeys)
	assert.Equal(t, 10, up.Priority)
	assert.True(t, up.Healthy)

	got, err := s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.Equal(t, up.ID, got.ID)
	assert.Equal(t, "openai", got.Name)
	assert.Equal(t, []string{"sk-key123"}, got.APIKeys)
}

func TestUpstream_GetNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetUpstream(9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpstream_List(t *testing.T) {
	s := newTestStore(t)

	_, err := s.CreateUpstream("provider-a", "https://a.example.com", []string{"key-a"}, 5, "", "", "", "")
	require.NoError(t, err)
	_, err = s.CreateUpstream("provider-b", "https://b.example.com", []string{"key-b"}, 10, "", "", "", "")
	require.NoError(t, err)

	list, err := s.ListUpstreams()
	require.NoError(t, err)
	assert.Len(t, list, 2)

	// Verify decrypted API keys are returned
	names := make([]string, len(list))
	for i, u := range list {
		names[i] = u.Name
		assert.NotEmpty(t, u.APIKeys)
	}
	assert.Contains(t, names, "provider-a")
	assert.Contains(t, names, "provider-b")
}

func TestUpstream_Update(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("old-name", "https://old.example.com", []string{"old-key"}, 1, "", "", "", "")
	require.NoError(t, err)

	updated, err := s.UpdateUpstream(up.ID, "new-name", "https://new.example.com", []string{"new-key"}, 2, true, "", "", "", "")
	require.NoError(t, err)
	assert.Equal(t, up.ID, updated.ID)
	assert.Equal(t, "new-name", updated.Name)
	assert.Equal(t, "https://new.example.com", updated.BaseURL)
	assert.Equal(t, []string{"new-key"}, updated.APIKeys)
	assert.Equal(t, 2, updated.Priority)
}

func TestUpstream_UpdateNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.UpdateUpstream(9999, "name", "https://example.com", []string{"key"}, 0, true, "", "", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpstream_Delete(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("to-delete", "https://example.com", []string{"key"}, 0, "", "", "", "")
	require.NoError(t, err)

	err = s.DeleteUpstream(up.ID)
	require.NoError(t, err)

	_, err = s.GetUpstream(up.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpstream_DeleteNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.DeleteUpstream(9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpstream_BatchSetEnabledAndDelete(t *testing.T) {
	s := newTestStore(t)
	u1, err := s.CreateUpstream("batch-a", "https://a.example.com", []string{"k1"}, 0, "", "", "", "")
	require.NoError(t, err)
	u2, err := s.CreateUpstream("batch-b", "https://b.example.com", []string{"k2"}, 0, "", "", "", "")
	require.NoError(t, err)
	u3, err := s.CreateUpstream("batch-c", "https://c.example.com", []string{"k3"}, 0, "", "", "", "")
	require.NoError(t, err)

	// All created enabled by default.
	n, err := s.BatchSetUpstreamEnabled([]int64{u1.ID, u2.ID, u1.ID, 0, -1}, false)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	got1, err := s.GetUpstream(u1.ID)
	require.NoError(t, err)
	assert.False(t, got1.Enabled)
	got2, err := s.GetUpstream(u2.ID)
	require.NoError(t, err)
	assert.False(t, got2.Enabled)
	got3, err := s.GetUpstream(u3.ID)
	require.NoError(t, err)
	assert.True(t, got3.Enabled)

	n, err = s.BatchSetUpstreamEnabled([]int64{u1.ID, u2.ID}, true)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	deleted, err := s.BatchDeleteUpstreams([]int64{u1.ID, u3.ID, u1.ID})
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)

	_, err = s.GetUpstream(u1.ID)
	require.Error(t, err)
	_, err = s.GetUpstream(u3.ID)
	require.Error(t, err)
	_, err = s.GetUpstream(u2.ID)
	require.NoError(t, err)

	// Empty / unknown ids are no-ops, not errors.
	n, err = s.BatchSetUpstreamEnabled(nil, false)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
	deleted, err = s.BatchDeleteUpstreams([]int64{99999})
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)
}

func TestUpstream_APIKeyNotStoredAsPlaintext(t *testing.T) {
	s := newTestStore(t)
	plainKey := "sk-plaintext-secret-key"

	up, err := s.CreateUpstream("test", "https://example.com", []string{plainKey}, 0, "", "", "", "")
	require.NoError(t, err)

	// Read the raw api_key column from the upstream_api_keys table directly.
	var rawStored string
	err = s.db.QueryRow(`SELECT api_key FROM upstream_api_keys WHERE upstream_id = ?`, up.ID).Scan(&rawStored)
	require.NoError(t, err)

	assert.NotEqual(t, plainKey, rawStored, "plaintext key must not be stored in the database")
	assert.True(t, strings.HasPrefix(rawStored, "v1:"), "stored value should have encryption version prefix")
}

func TestUpstream_AddAndDeleteAPIKey(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://example.com", []string{"key-a"}, 0, "", "", "", "")
	require.NoError(t, err)

	keys, err := s.AddUpstreamAPIKeys(up.ID, []string{"key-b", "key-c"})
	require.NoError(t, err)
	require.Len(t, keys, 3)
	assert.Equal(t, "key-a", keys[0].Key)
	assert.Equal(t, "key-b", keys[1].Key)
	assert.Equal(t, "key-c", keys[2].Key)

	err = s.DeleteUpstreamAPIKey(up.ID, keys[1].RowID)
	require.NoError(t, err)

	got, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"key-a", "key-c"}, []string{got[0].Key, got[1].Key})
}

func TestUpstream_DeleteAPIKeyNotFound(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://example.com", []string{"key"}, 0, "", "", "", "")
	require.NoError(t, err)

	err = s.DeleteUpstreamAPIKey(up.ID, 9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpstream_AddAPIKeyNotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.AddUpstreamAPIKeys(9999, []string{"key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

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

	updated, err := s.UpdateKey(dk.ID, "renamed", 99, false)
	require.NoError(t, err)
	assert.Equal(t, dk.ID, updated.ID)
	assert.Equal(t, "renamed", updated.Name)
	assert.Equal(t, 99, updated.RPMLimit)
	assert.False(t, updated.Enabled)
}

func TestKey_UpdateNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.UpdateKey(9999, "name", 0, true)
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
// Request Logs
// ---------------------------------------------------------------------------

func TestRequestLog_InsertBatchAndQuery(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("log-key", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	logs := []RequestLog{
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/chat/completions", StatusCode: 200, LatencyMs: 150, CreatedAt: now},
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/chat/completions", StatusCode: 429, LatencyMs: 10, CreatedAt: now.Add(-time.Minute)},
	}

	err = s.InsertRequestLogBatch(logs)
	require.NoError(t, err)

	results, err := s.QueryLogs(dk.ID, now.Add(-time.Hour), now.Add(time.Hour), 100)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestRequestLog_InsertBatch_Empty(t *testing.T) {
	s := newTestStore(t)
	err := s.InsertRequestLogBatch(nil)
	assert.NoError(t, err)

	err = s.InsertRequestLogBatch([]RequestLog{})
	assert.NoError(t, err)
}

func TestRequestLog_QueryLogs_AllKeys(t *testing.T) {
	s := newTestStore(t)

	_, dk1, err := s.CreateKey("key-1", 0)
	require.NoError(t, err)
	_, dk2, err := s.CreateKey("key-2", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	logs := []RequestLog{
		{DownstreamKeyID: dk1.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 50, CreatedAt: now},
		{DownstreamKeyID: dk2.ID, ProviderStyle: "anthropic", Path: "/v1/messages", StatusCode: 200, LatencyMs: 80, CreatedAt: now},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	// keyID=0 means all keys
	results, err := s.QueryLogs(0, now.Add(-time.Hour), now.Add(time.Hour), 0)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestRequestLog_QueryLogs_FilterByKey(t *testing.T) {
	s := newTestStore(t)

	_, dk1, err := s.CreateKey("key-a", 0)
	require.NoError(t, err)
	_, dk2, err := s.CreateKey("key-b", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	logs := []RequestLog{
		{DownstreamKeyID: dk1.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 50, CreatedAt: now},
		{DownstreamKeyID: dk2.ID, ProviderStyle: "anthropic", Path: "/v1/messages", StatusCode: 200, LatencyMs: 80, CreatedAt: now},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	results, err := s.QueryLogs(dk1.ID, now.Add(-time.Hour), now.Add(time.Hour), 0)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, dk1.ID, results[0].DownstreamKeyID)
}

func TestRequestLog_QueryLogs_Limit(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("limit-key", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	logs := make([]RequestLog, 5)
	for i := range logs {
		logs[i] = RequestLog{
			DownstreamKeyID: dk.ID,
			ProviderStyle:   "openai",
			Path:            "/v1/chat",
			StatusCode:      200,
			LatencyMs:       int64(i * 10),
			CreatedAt:       now.Add(time.Duration(i) * time.Second),
		}
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	results, err := s.QueryLogs(dk.ID, now.Add(-time.Hour), now.Add(time.Hour), 3)
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestDeleteLogsOlderThan(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("old-log-key", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-30 * time.Minute)

	logs := []RequestLog{
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 10, CreatedAt: old},
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 10, CreatedAt: recent},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	// Delete logs older than 24 hours — should remove the 48h-old entry
	err = s.DeleteLogsOlderThan(24 * time.Hour)
	require.NoError(t, err)

	results, err := s.QueryLogs(dk.ID, now.Add(-72*time.Hour), now.Add(time.Hour), 0)
	require.NoError(t, err)
	assert.Len(t, results, 1, "only the recent log should remain")
	assert.Equal(t, recent.Unix(), results[0].CreatedAt.Unix())
}

// ---------------------------------------------------------------------------
// Key-Upstream Bindings
// ---------------------------------------------------------------------------

func TestKeyUpstreamBinding_SetAndGet(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("bound-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("upstream-1", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "")
	require.NoError(t, err)
	u2, err := s.CreateUpstream("upstream-2", "https://b.example.com", []string{"key-b"}, 0, "", "", "", "")
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
	u1, err := s.CreateUpstream("up-1", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "")
	require.NoError(t, err)
	u2, err := s.CreateUpstream("up-2", "https://b.example.com", []string{"key-b"}, 0, "", "", "", "")
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
	u1, err := s.CreateUpstream("up-c", "https://c.example.com", []string{"key-c"}, 0, "", "", "", "")
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
	u1, err := s.CreateUpstream("up-d", "https://d.example.com", []string{"key-d"}, 0, "", "", "", "")
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
	u1, err := s.CreateUpstream("up-e", "https://e.example.com", []string{"key-e"}, 0, "", "", "", "")
	require.NoError(t, err)
	u2, err := s.CreateUpstream("up-f", "https://f.example.com", []string{"key-f"}, 0, "", "", "", "")
	require.NoError(t, err)

	err = s.SetKeyUpstreams(dk.ID, []int64{u1.ID, u2.ID})
	require.NoError(t, err)

	// Delete one upstream — that binding should cascade delete
	err = s.DeleteUpstream(u1.ID)
	require.NoError(t, err)

	ids, err := s.GetKeyUpstreamIDs(dk.ID)
	require.NoError(t, err)
	assert.Len(t, ids, 1)
	assert.Equal(t, u2.ID, ids[0])
}

// ---------------------------------------------------------------------------
// CountLogsSince
// ---------------------------------------------------------------------------

func TestCountLogsSince_Empty(t *testing.T) {
	s := newTestStore(t)
	count, err := s.CountLogsSince(time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCountLogsSince_InclusiveBoundary(t *testing.T) {
	s := newTestStore(t)
	_, dk, _ := s.CreateKey("count-key", 0)

	exact := time.Now().UTC().Truncate(time.Second)
	logs := []RequestLog{
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 10, CreatedAt: exact},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	count, err := s.CountLogsSince(exact)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "log at exact boundary should be counted")
}

func TestCountLogsSince_MixedOldAndRecent(t *testing.T) {
	s := newTestStore(t)
	_, dk, _ := s.CreateKey("mixed-key", 0)

	now := time.Now().UTC()
	logs := []RequestLog{
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/a", StatusCode: 200, LatencyMs: 10, CreatedAt: now.Add(-2 * time.Hour)},
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/b", StatusCode: 200, LatencyMs: 10, CreatedAt: now.Add(-30 * time.Minute)},
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/c", StatusCode: 200, LatencyMs: 10, CreatedAt: now},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	count, err := s.CountLogsSince(now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, count, "should count only logs within last hour")
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
	_, err := s.UpdateKey(dk.ID, "disabled-key", 0, false)
	require.NoError(t, err)

	count, err := s.CountKeys()
	require.NoError(t, err)
	assert.Equal(t, 1, count, "disabled keys should still be counted")
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
	u1, _ := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "")
	u2, _ := s.CreateUpstream("u2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "")
	u3, _ := s.CreateUpstream("u3", "https://c.example.com", []string{"kc"}, 0, "", "", "", "")

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
	u1, _ := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "")
	u2, _ := s.CreateUpstream("u2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "")

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
	u, err := s.CreateUpstream("test-up", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "")
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
	u, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "")
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
	u, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "")
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
	u, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "")
	require.NoError(t, err)

	err = s.SetUpstreamModelPatterns(u.ID, []string{"claude-*", "gpt-*"})
	require.NoError(t, err)

	err = s.DeleteUpstream(u.ID)
	require.NoError(t, err)

	patterns, err := s.GetUpstreamModelPatterns(u.ID)
	require.NoError(t, err)
	assert.Empty(t, patterns, "patterns should cascade delete with upstream")
}

func TestUpstreamModelPatterns_GetAll(t *testing.T) {
	s := newTestStore(t)
	u1, _ := s.CreateUpstream("up1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "")
	u2, _ := s.CreateUpstream("up2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "")

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
	u, _ := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "")

	patterns, err := s.GetUpstreamModelPatterns(u.ID)
	require.NoError(t, err)
	assert.Empty(t, patterns, "no patterns by default")
}

// ---------------------------------------------------------------------------
// ResetKeyFailures
// ---------------------------------------------------------------------------

func TestResetKeyFailures_AfterIncrement(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "")
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
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "")
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
// SetSetting / GetSetting
// ---------------------------------------------------------------------------

func TestSetting_SetAndGet(t *testing.T) {
	s := newTestStore(t)

	err := s.SetSetting("my_key", "my_value")
	require.NoError(t, err)

	val, err := s.GetSetting("my_key", "default")
	require.NoError(t, err)
	assert.Equal(t, "my_value", val)
}

func TestSetting_GetMissing_ReturnsDefault(t *testing.T) {
	s := newTestStore(t)

	val, err := s.GetSetting("nonexistent", "fallback")
	require.NoError(t, err)
	assert.Equal(t, "fallback", val)
}

func TestSetting_Upsert(t *testing.T) {
	s := newTestStore(t)

	err := s.SetSetting("threshold", "5")
	require.NoError(t, err)

	err = s.SetSetting("threshold", "10")
	require.NoError(t, err)

	val, err := s.GetSetting("threshold", "0")
	require.NoError(t, err)
	assert.Equal(t, "10", val)
}

func TestSetting_EmptyValue(t *testing.T) {
	s := newTestStore(t)

	err := s.SetSetting("empty", "")
	require.NoError(t, err)

	val, err := s.GetSetting("empty", "default")
	require.NoError(t, err)
	assert.Equal(t, "", val, "empty string should be stored, not treated as missing")
}

func TestSetting_MultipleKeys(t *testing.T) {
	s := newTestStore(t)

	require.NoError(t, s.SetSetting("a", "1"))
	require.NoError(t, s.SetSetting("b", "2"))
	require.NoError(t, s.SetSetting("c", "3"))

	v1, _ := s.GetSetting("a", "")
	v2, _ := s.GetSetting("b", "")
	v3, _ := s.GetSetting("c", "")
	assert.Equal(t, "1", v1)
	assert.Equal(t, "2", v2)
	assert.Equal(t, "3", v3)
}

// ---------------------------------------------------------------------------
// AutoDisableFailingKeys
// ---------------------------------------------------------------------------

func TestAutoDisableFailingKeys_DisablesAboveThreshold(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"key-a", "key-b", "key-c"}, 0, "", "", "", "")
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
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "")
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
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "")
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
// GetKeyUsageStats
// ---------------------------------------------------------------------------

func TestGetKeyUsageStats_Empty(t *testing.T) {
	s := newTestStore(t)
	stats, err := s.GetKeyUsageStats()
	require.NoError(t, err)
	assert.Empty(t, stats)
}

func TestGetKeyUsageStats_Aggregation(t *testing.T) {
	s := newTestStore(t)
	_, dk1, err := s.CreateKey("stats-key-1", 0)
	require.NoError(t, err)
	_, dk2, err := s.CreateKey("stats-key-2", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	logs := []RequestLog{
		// dk1: 2 success (200), 1 error (500)
		{DownstreamKeyID: dk1.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 100, CreatedAt: now},
		{DownstreamKeyID: dk1.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 200, CreatedAt: now},
		{DownstreamKeyID: dk1.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 500, LatencyMs: 300, CreatedAt: now},
		// dk2: 1 success
		{DownstreamKeyID: dk2.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 50, CreatedAt: now},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	stats, err := s.GetKeyUsageStats()
	require.NoError(t, err)
	require.Len(t, stats, 2)

	// Results ordered by total DESC, so dk1 (3 total) comes first
	assert.Equal(t, dk1.ID, stats[0].KeyID)
	assert.Equal(t, 3, stats[0].Total)
	assert.Equal(t, 2, stats[0].Success)
	assert.Equal(t, 1, stats[0].Error)
	assert.InDelta(t, 200.0, stats[0].AvgLatencyMs, 0.1) // (100+200+300)/3

	assert.Equal(t, dk2.ID, stats[1].KeyID)
	assert.Equal(t, 1, stats[1].Total)
	assert.Equal(t, 1, stats[1].Success)
	assert.Equal(t, 0, stats[1].Error)
	assert.InDelta(t, 50.0, stats[1].AvgLatencyMs, 0.1)
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
	u1, err := s.CreateUpstream("up1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "")
	require.NoError(t, err)
	u2, err := s.CreateUpstream("up2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "")
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
	u1, err := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "")
	require.NoError(t, err)
	u2, err := s.CreateUpstream("u2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "")
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
	u1, err := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "")
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
	u1, err := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "")
	require.NoError(t, err)
	u2, err := s.CreateUpstream("u2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "")
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
	u1, err := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "")
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
// InsertRequestLogBatch (additional coverage for extra fields)
// ---------------------------------------------------------------------------

func TestInsertRequestLogBatch_AllFields(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("log-key", 0)
	require.NoError(t, err)

	now := time.Now().UTC()
	logs := []RequestLog{
		{
			DownstreamKeyID: dk.ID,
			UpstreamName:    "openai-prod",
			UpstreamKeyIdx:  2,
			Model:           "gpt-4o",
			UsedProxy:       "http://proxy.example.com:8080",
			ClientIP:        "1.2.3.4",
			IPRegion:        "US",
			ProviderStyle:   "openai",
			Path:            "/v1/chat/completions",
			StatusCode:      200,
			LatencyMs:       150,
			CreatedAt:       now,
		},
	}
	err = s.InsertRequestLogBatch(logs)
	require.NoError(t, err)

	results, err := s.QueryLogs(dk.ID, now.Add(-time.Minute), now.Add(time.Minute), 0)
	require.NoError(t, err)
	require.Len(t, results, 1)

	r := results[0]
	assert.Equal(t, "openai-prod", r.UpstreamName)
	assert.Equal(t, 2, r.UpstreamKeyIdx)
	assert.Equal(t, "gpt-4o", r.Model)
	assert.Equal(t, "http://proxy.example.com:8080", r.UsedProxy)
	assert.Equal(t, "1.2.3.4", r.ClientIP)
	assert.Equal(t, "US", r.IPRegion)
}

func TestInsertRequestLogBatch_ZeroCreatedAt(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("zero-ts-key", 0)
	require.NoError(t, err)

	logs := []RequestLog{
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 10},
		// CreatedAt is zero
	}
	err = s.InsertRequestLogBatch(logs)
	require.NoError(t, err)

	// Should have auto-filled CreatedAt, so query with a wide window should find it
	results, err := s.QueryLogs(dk.ID, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 0)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.False(t, results[0].CreatedAt.IsZero(), "zero CreatedAt should be auto-filled")
}

// ---------------------------------------------------------------------------
// IncrKeyFailures (dedicated deeper coverage)
// ---------------------------------------------------------------------------

func TestIncrKeyFailures_ReturnsNewCount(t *testing.T) {
	s := newTestStore(t)
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "")
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
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"key-a"}, 0, "", "", "", "")
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
	u1, err := s.CreateUpstream("up1", "https://a.example.com", []string{"k1", "k2"}, 0, "", "", "", "")
	require.NoError(t, err)
	u2, err := s.CreateUpstream("up2", "https://b.example.com", []string{"k3"}, 0, "", "", "", "")
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
	up, err := s.CreateUpstream("up", "https://a.example.com", []string{"k1", "k2"}, 0, "", "", "", "")
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

	_, err = s.UpdateKey(dk.ID, "disabled-key", 0, false)
	require.NoError(t, err)

	found, err := s.LookupKeyByID(dk.ID)
	require.NoError(t, err)
	assert.Equal(t, dk.ID, found.ID)
	assert.False(t, found.Enabled, "should return the key even when disabled")
}

// ---------------------------------------------------------------------------
// Upstream Declared Models
// ---------------------------------------------------------------------------

func TestUpstreamDeclaredModels_SetAndGet(t *testing.T) {
	s := newTestStore(t)
	u, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "")
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
	u, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "")
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
	u, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "")
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
	u, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "")
	require.NoError(t, err)

	models, err := s.GetUpstreamDeclaredModels(u.ID)
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestUpstreamDeclaredModels_CascadeDeleteUpstream(t *testing.T) {
	s := newTestStore(t)
	u, err := s.CreateUpstream("up", "https://a.example.com", []string{"key"}, 0, "", "", "", "")
	require.NoError(t, err)

	err = s.SetUpstreamDeclaredModels(u.ID, []string{"gpt-4o", "gpt-4o-mini"})
	require.NoError(t, err)

	err = s.DeleteUpstream(u.ID)
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
	u1, err := s.CreateUpstream("up1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "")
	require.NoError(t, err)
	u2, err := s.CreateUpstream("up2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "")
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
	u1, err := s.CreateUpstream("up1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "")
	require.NoError(t, err)
	u2, err := s.CreateUpstream("up2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "")
	require.NoError(t, err)

	require.NoError(t, s.SetUpstreamDeclaredModels(u1.ID, []string{"gpt-4o"}))
	require.NoError(t, s.SetUpstreamDeclaredModels(u2.ID, []string{"claude-sonnet-4-20250514"}))

	// Disable u1
	_, err = s.UpdateUpstream(u1.ID, "up1", "https://a.example.com", nil, 0, false, "", "", "", "")
	require.NoError(t, err)

	all, err := s.GetAllUpstreamDeclaredModels()
	require.NoError(t, err)
	assert.Empty(t, all[u1.ID], "disabled upstream should not appear in GetAllUpstreamDeclaredModels")
	assert.Len(t, all[u2.ID], 1)
}

// ---------------------------------------------------------------------------
// Test Models CRUD
// ---------------------------------------------------------------------------

func TestTestModel_CreateAndList(t *testing.T) {
	s := newTestStore(t)

	m1, err := s.CreateTestModel("gpt-4o", "openai")
	require.NoError(t, err)
	require.NotNil(t, m1)
	assert.Positive(t, m1.ID)
	assert.Equal(t, "gpt-4o", m1.Name)
	assert.Equal(t, "openai", m1.Protocol)
	assert.False(t, m1.CreatedAt.IsZero())

	m2, err := s.CreateTestModel("claude-sonnet-4-20250514", "anthropic")
	require.NoError(t, err)
	require.NotNil(t, m2)

	// List all
	all, err := s.ListTestModels("")
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestTestModel_ListByProtocol(t *testing.T) {
	s := newTestStore(t)

	_, err := s.CreateTestModel("gpt-4o", "openai")
	require.NoError(t, err)
	_, err = s.CreateTestModel("gpt-4o-mini", "openai")
	require.NoError(t, err)
	_, err = s.CreateTestModel("claude-sonnet-4-20250514", "anthropic")
	require.NoError(t, err)

	openaiModels, err := s.ListTestModels("openai")
	require.NoError(t, err)
	assert.Len(t, openaiModels, 2)
	for _, m := range openaiModels {
		assert.Equal(t, "openai", m.Protocol)
	}

	anthropicModels, err := s.ListTestModels("anthropic")
	require.NoError(t, err)
	assert.Len(t, anthropicModels, 1)
	assert.Equal(t, "claude-sonnet-4-20250514", anthropicModels[0].Name)
}

func TestTestModel_ListEmpty(t *testing.T) {
	s := newTestStore(t)
	models, err := s.ListTestModels("")
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestTestModel_Update(t *testing.T) {
	s := newTestStore(t)

	m, err := s.CreateTestModel("old-model", "openai")
	require.NoError(t, err)

	err = s.UpdateTestModel(m.ID, "new-model", "anthropic")
	require.NoError(t, err)

	// Verify the update
	models, err := s.ListTestModels("")
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "new-model", models[0].Name)
	assert.Equal(t, "anthropic", models[0].Protocol)
}

func TestTestModel_UpdateNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.UpdateTestModel(9999, "name", "openai")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestTestModel_Delete(t *testing.T) {
	s := newTestStore(t)

	m, err := s.CreateTestModel("to-delete", "openai")
	require.NoError(t, err)

	err = s.DeleteTestModel(m.ID)
	require.NoError(t, err)

	models, err := s.ListTestModels("")
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestTestModel_DeleteNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.DeleteTestModel(9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------------------------------------------------------------------------
// SetKeyModelOverrides — deeper coverage (cascade behavior)
// ---------------------------------------------------------------------------

func TestKeyModelOverrides_CascadeDeleteKey(t *testing.T) {
	s := newTestStore(t)
	_, dk, err := s.CreateKey("cascade-override-key", 0)
	require.NoError(t, err)
	u1, err := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "")
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
	u1, err := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "")
	require.NoError(t, err)
	u2, err := s.CreateUpstream("u2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "")
	require.NoError(t, err)

	err = s.SetKeyModelOverrides(dk.ID, []KeyModelOverrideInput{
		{ModelPattern: "claude-*", UpstreamID: u1.ID},
		{ModelPattern: "gpt-*", UpstreamID: u2.ID},
	})
	require.NoError(t, err)

	// Delete u1 — only its override should cascade
	err = s.DeleteUpstream(u1.ID)
	require.NoError(t, err)

	got, err := s.GetKeyModelOverrides(dk.ID)
	require.NoError(t, err)
	require.Len(t, got, 1, "only the override for u2 should remain")
	assert.Equal(t, "gpt-*", got[0].ModelPattern)
	assert.Equal(t, u2.ID, got[0].UpstreamID)
}

// ===========================================================================
// EDGE-CASE / COVERAGE TESTS
// ===========================================================================

// ---------------------------------------------------------------------------
// CreateUpstream — edge cases
// ---------------------------------------------------------------------------

func TestCreateUpstream_EmptyAPIKeys_PublicUpstream(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("public", "https://public.example.com", []string{}, 5, "", "", "", "")
	require.NoError(t, err)
	assert.Empty(t, up.APIKeys, "public upstream should have no API keys")
	assert.Equal(t, 5, up.Priority)
	assert.True(t, up.Enabled)

	got, err := s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.Empty(t, got.APIKeys)
}

func TestCreateUpstream_NilAPIKeys_PublicUpstream(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("nil-keys", "https://nil.example.com", nil, 0, "", "", "", "")
	require.NoError(t, err)
	assert.Empty(t, up.APIKeys)
}

func TestCreateUpstream_MultipleAPIKeys(t *testing.T) {
	s := newTestStore(t)

	keys := []string{"sk-key1", "sk-key2", "sk-key3"}
	up, err := s.CreateUpstream("multi-key", "https://multi.example.com", keys, 0, "", "", "", "")
	require.NoError(t, err)
	assert.Equal(t, keys, up.APIKeys)

	got, err := s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.Equal(t, keys, got.APIKeys)
}

func TestCreateUpstream_WithRemark(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("remarked", "https://r.example.com", []string{"k"}, 0, "", "", "", "donated by Alice")
	require.NoError(t, err)
	assert.Equal(t, "donated by Alice", up.Remark)

	got, err := s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.Equal(t, "donated by Alice", got.Remark)
}

func TestCreateUpstream_WithProxyURL(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("proxied", "https://p.example.com", []string{"k"}, 0, "http://proxy:8080", "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "http://proxy:8080", up.ProxyURL)

	got, err := s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.Equal(t, "http://proxy:8080", got.ProxyURL)
}

func TestCreateUpstream_AllAuthModes(t *testing.T) {
	s := newTestStore(t)

	// Default (empty string should become "api_key")
	up1, err := s.CreateUpstream("default-auth", "https://a.example.com", []string{"k"}, 0, "", "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "api_key", up1.AuthMode)

	// Explicit api_key
	up2, err := s.CreateUpstream("api-key-auth", "https://b.example.com", []string{"k"}, 0, "", "", "api_key", "")
	require.NoError(t, err)
	assert.Equal(t, "api_key", up2.AuthMode)

	// OAuth
	up3, err := s.CreateUpstream("oauth-auth", "https://c.example.com", []string{"k"}, 0, "", "", "oauth", "")
	require.NoError(t, err)
	assert.Equal(t, "oauth", up3.AuthMode)

	got, err := s.GetUpstream(up3.ID)
	require.NoError(t, err)
	assert.Equal(t, "oauth", got.AuthMode)
}

func TestCreateUpstream_DefaultKeySchedulingMode(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("sched-default", "https://s.example.com", []string{"k"}, 0, "", "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "round-robin", up.KeySchedulingMode)

	up2, err := s.CreateUpstream("sched-fill", "https://s2.example.com", []string{"k"}, 0, "", "fill", "", "")
	require.NoError(t, err)
	assert.Equal(t, "fill", up2.KeySchedulingMode)
}

// ---------------------------------------------------------------------------
// UpdateUpstream — edge cases
// ---------------------------------------------------------------------------

func TestUpdateUpstream_UpdateBaseURL(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://old.example.com", []string{"k"}, 0, "", "", "", "")
	require.NoError(t, err)

	updated, err := s.UpdateUpstream(up.ID, "up", "https://new.example.com", nil, 0, true, "", "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "https://new.example.com", updated.BaseURL)
	// apiKeys=nil means keep existing keys
	assert.Equal(t, []string{"k"}, updated.APIKeys)
}

func TestUpdateUpstream_UpdateProxyURL(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k"}, 0, "", "", "", "")
	require.NoError(t, err)

	updated, err := s.UpdateUpstream(up.ID, "up", "https://u.example.com", nil, 0, true, "socks5://proxy:1080", "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "socks5://proxy:1080", updated.ProxyURL)
}

func TestUpdateUpstream_ClearProxyURL(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k"}, 0, "http://proxy:8080", "", "", "")
	require.NoError(t, err)

	updated, err := s.UpdateUpstream(up.ID, "up", "https://u.example.com", nil, 0, true, "", "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "", updated.ProxyURL, "proxy_url should be cleared")
}

func TestUpdateUpstream_UpdateRemark(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k"}, 0, "", "", "", "old remark")
	require.NoError(t, err)

	updated, err := s.UpdateUpstream(up.ID, "up", "https://u.example.com", nil, 0, true, "", "", "", "new remark")
	require.NoError(t, err)
	assert.Equal(t, "new remark", updated.Remark)
}

func TestUpdateUpstream_UpdatePriority(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k"}, 1, "", "", "", "")
	require.NoError(t, err)

	updated, err := s.UpdateUpstream(up.ID, "up", "https://u.example.com", nil, 99, true, "", "", "", "")
	require.NoError(t, err)
	assert.Equal(t, 99, updated.Priority)
}

func TestUpdateUpstream_UpdateModelPatterns_ViaUpdate(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k"}, 0, "", "", "", "")
	require.NoError(t, err)

	// Set patterns separately then update upstream metadata
	require.NoError(t, s.SetUpstreamModelPatterns(up.ID, []string{"gpt-*"}))

	updated, err := s.UpdateUpstream(up.ID, "renamed", "https://u.example.com", nil, 5, true, "", "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "renamed", updated.Name)

	// Patterns should still exist after upstream metadata update
	patterns, err := s.GetUpstreamModelPatterns(up.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-*"}, patterns)
}

func TestUpdateUpstream_NilAPIKeysKeepsExisting(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"key-a", "key-b"}, 0, "", "", "", "")
	require.NoError(t, err)

	updated, err := s.UpdateUpstream(up.ID, "up", "https://u.example.com", nil, 0, true, "", "", "", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"key-a", "key-b"}, updated.APIKeys, "nil apiKeys should preserve existing keys")
}

// ---------------------------------------------------------------------------
// AddUpstreamAPIKeys — edge cases
// ---------------------------------------------------------------------------

func TestAddUpstreamAPIKeys_EmptyKeyList(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"existing-key"}, 0, "", "", "", "")
	require.NoError(t, err)

	keys, err := s.AddUpstreamAPIKeys(up.ID, []string{})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, "existing-key", keys[0].Key)
}

func TestAddUpstreamAPIKeys_AddToUpstreamWithExistingKeys(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k1", "k2"}, 0, "", "", "", "")
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

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k"}, 0, "", "", "", "")
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

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k"}, 0, "", "", "", "")
	require.NoError(t, err)

	err = s.DeleteUpstreamAPIKey(up.ID, 9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDeleteUpstreamAPIKey_WrongUpstreamID(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("up", "https://u.example.com", []string{"k"}, 0, "", "", "", "")
	require.NoError(t, err)

	keys, err := s.GetUpstreamAllAPIKeys(up.ID)
	require.NoError(t, err)

	// Try to delete with wrong upstream ID
	err = s.DeleteUpstreamAPIKey(9999, keys[0].RowID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------------------------------------------------------------------------
// SetKeyUpstreams — edge cases
// ---------------------------------------------------------------------------

func TestSetKeyUpstreams_SetMultiple(t *testing.T) {
	s := newTestStore(t)

	_, dk, err := s.CreateKey("k", 0)
	require.NoError(t, err)
	u1, _ := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "")
	u2, _ := s.CreateUpstream("u2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "")
	u3, _ := s.CreateUpstream("u3", "https://c.example.com", []string{"kc"}, 0, "", "", "", "")

	err = s.SetKeyUpstreams(dk.ID, []int64{u1.ID, u2.ID, u3.ID})
	require.NoError(t, err)

	ids, err := s.GetKeyUpstreamIDs(dk.ID)
	require.NoError(t, err)
	assert.Len(t, ids, 3)
}

func TestSetKeyUpstreams_SetForNonExistentKey(t *testing.T) {
	s := newTestStore(t)

	u1, _ := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "")

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
	u1, _ := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "")
	u2, _ := s.CreateUpstream("u2", "https://b.example.com", []string{"kb"}, 0, "", "", "", "")
	u3, _ := s.CreateUpstream("u3", "https://c.example.com", []string{"kc"}, 0, "", "", "", "")

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
	u1, _ := s.CreateUpstream("u1", "https://a.example.com", []string{"ka"}, 0, "", "", "", "")

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
// Encrypt / Decrypt — additional edge cases
// ---------------------------------------------------------------------------

func TestEncrypt_LongPlaintext(t *testing.T) {
	// 4 KB plaintext
	plaintext := strings.Repeat("a]1[!@#$%^&*()", 300)
	ciphertext, err := Encrypt(plaintext, testKey)
	require.NoError(t, err)

	decrypted, err := Decrypt(ciphertext, testKey)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncrypt_WrongKeyLength(t *testing.T) {
	_, err := Encrypt("text", []byte("short"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}

func TestDecrypt_WrongKeyLength(t *testing.T) {
	_, err := Decrypt("v1:dGVzdA==", []byte("short"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}

func TestDecrypt_InvalidBase64(t *testing.T) {
	_, err := Decrypt("v1:not-valid-base64!!!", testKey)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base64")
}

func TestDecrypt_CiphertextTooShort(t *testing.T) {
	// v1: prefix + very short base64 that decodes to fewer bytes than nonce size
	shortData := "v1:YQ=="
	_, err := Decrypt(shortData, testKey)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too short")
}

func TestDecrypt_TamperedCiphertext(t *testing.T) {
	ciphertext, err := Encrypt("secret-data", testKey)
	require.NoError(t, err)

	// Tamper with the ciphertext (flip a character in the base64 payload)
	parts := strings.SplitN(ciphertext, ":", 2)
	require.Len(t, parts, 2)
	payload := []byte(parts[1])
	if payload[len(payload)-2] == 'A' {
		payload[len(payload)-2] = 'B'
	} else {
		payload[len(payload)-2] = 'A'
	}
	tampered := "v1:" + string(payload)

	_, err = Decrypt(tampered, testKey)
	require.Error(t, err, "tampered ciphertext should fail GCM authentication")
}

// ---------------------------------------------------------------------------
// RunMigrations — verify tables
// ---------------------------------------------------------------------------

func TestRunMigrations_AllTablesExist(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migrate-tables.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()

	err = RunMigrations(db)
	require.NoError(t, err)

	// Verify expected tables exist by querying them
	expectedTables := []string{
		"_meta",
		"upstream_providers",
		"downstream_keys",
		"request_logs",
		"model_whitelist",
		"key_upstream_bindings",
		"upstream_model_patterns",
		"key_model_overrides",
		"upstream_api_keys",
		"test_models",
		"settings",
		"upstream_declared_models",
	}
	for _, table := range expectedTables {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		require.NoError(t, err, "table %q should exist", table)
		assert.Equal(t, table, name)
	}

	// Verify schema version is at latest
	var version int
	err = db.QueryRow(`SELECT schema_version FROM _meta`).Scan(&version)
	require.NoError(t, err)
	assert.Equal(t, 21, version, "schema version should be at latest migration")
}

// ---------------------------------------------------------------------------
// InsertRequestLogBatch — edge cases
// ---------------------------------------------------------------------------

func TestInsertRequestLogBatch_NilIsNoOp(t *testing.T) {
	s := newTestStore(t)
	err := s.InsertRequestLogBatch(nil)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// DeleteLogsOlderThan — edge cases
// ---------------------------------------------------------------------------

func TestDeleteLogsOlderThan_NoMatchingLogs(t *testing.T) {
	s := newTestStore(t)
	_, dk, _ := s.CreateKey("k", 0)

	now := time.Now().UTC()
	logs := []RequestLog{
		{DownstreamKeyID: dk.ID, ProviderStyle: "openai", Path: "/v1/chat", StatusCode: 200, LatencyMs: 10, CreatedAt: now},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	// Delete logs older than 1 hour — none should match
	err := s.DeleteLogsOlderThan(time.Hour)
	require.NoError(t, err)

	results, err := s.QueryLogs(dk.ID, now.Add(-time.Minute), now.Add(time.Minute), 0)
	require.NoError(t, err)
	assert.Len(t, results, 1, "recent log should still exist")
}

func TestDeleteLogsOlderThan_EmptyDatabase(t *testing.T) {
	s := newTestStore(t)

	// Should be a no-op on empty table, no error
	err := s.DeleteLogsOlderThan(time.Hour)
	require.NoError(t, err)
}
