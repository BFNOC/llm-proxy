package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

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

func TestMigration31_PreservesSelectedScopeAndDefaultsEmptyScopeToAll(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		insertSelected  bool
		expectedAllKeys bool
	}{
		{name: "selected scope", insertSelected: true, expectedAllKeys: false},
		{name: "empty legacy scope", insertSelected: false, expectedAllKeys: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "migrate-v31.db")
			db, err := sql.Open("sqlite", dbPath)
			require.NoError(t, err)
			defer db.Close()

			require.NoError(t, runMigrationsThrough(db, 30))
			if testCase.insertSelected {
				_, err = db.Exec(`INSERT INTO downstream_keys (key_hash, key_prefix, name, rpm_limit, enabled) VALUES ('hash', 'prefix', 'selected', 0, 1)`)
				require.NoError(t, err)
				var keyID int64
				require.NoError(t, db.QueryRow(`SELECT id FROM downstream_keys WHERE name = 'selected'`).Scan(&keyID))
				_, err = db.Exec(`INSERT INTO full_recording_keys (downstream_key_id) VALUES (?)`, keyID)
				require.NoError(t, err)
			}

			require.NoError(t, RunMigrations(db))
			var allKeys bool
			require.NoError(t, db.QueryRow(`SELECT record_all_keys FROM full_recording_config WHERE id = 1`).Scan(&allKeys))
			assert.Equal(t, testCase.expectedAllKeys, allKeys)
		})
	}
}

func runMigrationsThrough(db *sql.DB, targetVersion int) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS _meta (schema_version INTEGER NOT NULL DEFAULT 0)`); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO _meta (schema_version) SELECT 0 WHERE NOT EXISTS (SELECT 1 FROM _meta)`); err != nil {
		return err
	}
	for _, item := range migrations {
		if item.version > targetVersion {
			break
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(item.up); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err = tx.Exec(`UPDATE _meta SET schema_version = ?`, item.version); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
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
		"full_recording_config",
		"full_recording_keys",
		"request_log_details",
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
	assert.Equal(t, 31, version, "schema version should be at latest migration")
}
