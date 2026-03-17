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

	up, err := s.CreateUpstream("openai", "https://api.openai.com", "sk-key123", 10)
	require.NoError(t, err)
	require.NotNil(t, up)
	assert.Positive(t, up.ID)
	assert.Equal(t, "openai", up.Name)
	assert.Equal(t, "https://api.openai.com", up.BaseURL)
	assert.Equal(t, "sk-key123", up.APIKey)
	assert.Equal(t, 10, up.Priority)
	assert.True(t, up.Healthy)

	got, err := s.GetUpstream(up.ID)
	require.NoError(t, err)
	assert.Equal(t, up.ID, got.ID)
	assert.Equal(t, "openai", got.Name)
	assert.Equal(t, "sk-key123", got.APIKey)
}

func TestUpstream_GetNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetUpstream(9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpstream_List(t *testing.T) {
	s := newTestStore(t)

	_, err := s.CreateUpstream("provider-a", "https://a.example.com", "key-a", 5)
	require.NoError(t, err)
	_, err = s.CreateUpstream("provider-b", "https://b.example.com", "key-b", 10)
	require.NoError(t, err)

	list, err := s.ListUpstreams()
	require.NoError(t, err)
	assert.Len(t, list, 2)

	// Verify decrypted API keys are returned
	names := make([]string, len(list))
	for i, u := range list {
		names[i] = u.Name
		assert.NotEmpty(t, u.APIKey)
	}
	assert.Contains(t, names, "provider-a")
	assert.Contains(t, names, "provider-b")
}

func TestUpstream_Update(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("old-name", "https://old.example.com", "old-key", 1)
	require.NoError(t, err)

	updated, err := s.UpdateUpstream(up.ID, "new-name", "https://new.example.com", "new-key", 2, true)
	require.NoError(t, err)
	assert.Equal(t, up.ID, updated.ID)
	assert.Equal(t, "new-name", updated.Name)
	assert.Equal(t, "https://new.example.com", updated.BaseURL)
	assert.Equal(t, "new-key", updated.APIKey)
	assert.Equal(t, 2, updated.Priority)
}

func TestUpstream_UpdateNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.UpdateUpstream(9999, "name", "https://example.com", "key", 0, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpstream_Delete(t *testing.T) {
	s := newTestStore(t)

	up, err := s.CreateUpstream("to-delete", "https://example.com", "key", 0)
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

func TestUpstream_APIKeyNotStoredAsPlaintext(t *testing.T) {
	s := newTestStore(t)
	plainKey := "sk-plaintext-secret-key"

	up, err := s.CreateUpstream("test", "https://example.com", plainKey, 0)
	require.NoError(t, err)

	// Read the raw api_key column from the DB directly.
	var rawStored string
	err = s.db.QueryRow(`SELECT api_key FROM upstream_providers WHERE id = ?`, up.ID).Scan(&rawStored)
	require.NoError(t, err)

	assert.NotEqual(t, plainKey, rawStored, "plaintext key must not be stored in the database")
	assert.True(t, strings.HasPrefix(rawStored, "v1:"), "stored value should have encryption version prefix")
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
	u1, err := s.CreateUpstream("upstream-1", "https://a.example.com", "key-a", 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("upstream-2", "https://b.example.com", "key-b", 0)
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
	u1, err := s.CreateUpstream("up-1", "https://a.example.com", "key-a", 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("up-2", "https://b.example.com", "key-b", 0)
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
	u1, err := s.CreateUpstream("up-c", "https://c.example.com", "key-c", 0)
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
	u1, err := s.CreateUpstream("up-d", "https://d.example.com", "key-d", 0)
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
	u1, err := s.CreateUpstream("up-e", "https://e.example.com", "key-e", 0)
	require.NoError(t, err)
	u2, err := s.CreateUpstream("up-f", "https://f.example.com", "key-f", 0)
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
	u1, _ := s.CreateUpstream("u1", "https://a.example.com", "ka", 0)
	u2, _ := s.CreateUpstream("u2", "https://b.example.com", "kb", 0)
	u3, _ := s.CreateUpstream("u3", "https://c.example.com", "kc", 0)

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
	u1, _ := s.CreateUpstream("u1", "https://a.example.com", "ka", 0)
	u2, _ := s.CreateUpstream("u2", "https://b.example.com", "kb", 0)

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
