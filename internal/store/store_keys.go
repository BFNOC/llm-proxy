package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// 下游 Key CRUD
// ---------------------------------------------------------------------------

// CreateKey 生成一个新的下游 API Key，存储其 SHA-256 哈希，
// 并仅返回一次明文 Key。
func (s *Store) CreateKey(name string, rpmLimit int) (plaintext string, key *DownstreamKey, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate key bytes: %w", err)
	}

	plaintext = "sk-" + hex.EncodeToString(raw)
	prefix := plaintext[:len("sk-")+8] // "sk-" + 前 8 位十六进制字符

	hashBytes := sha256.Sum256([]byte(plaintext))
	keyHash := hex.EncodeToString(hashBytes[:])

	// 加密存储明文密钥，支持二次复制
	encrypted, err := Encrypt(plaintext, s.encryptionKey)
	if err != nil {
		return "", nil, fmt.Errorf("encrypt key plaintext: %w", err)
	}

	now := time.Now().UTC()
	res, err := s.db.Exec(
		`INSERT INTO downstream_keys (key_hash, key_prefix, name, rpm_limit, max_concurrent, enabled, key_encrypted, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 0, 1, ?, ?, ?)`,
		keyHash, prefix, name, rpmLimit, encrypted, now, now,
	)
	if err != nil {
		return "", nil, fmt.Errorf("insert downstream key: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return "", nil, fmt.Errorf("last insert id: %w", err)
	}

	key = &DownstreamKey{
		ID:        id,
		KeyHash:   keyHash,
		KeyPrefix: prefix,
		Name:      name,
		RPMLimit:  rpmLimit,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return plaintext, key, nil
}

// GetKeyPlaintext 解密并返回下游密钥的明文。
// 旧密钥（v12 迁移前创建的）返回空字符串和 nil 错误。
func (s *Store) GetKeyPlaintext(id int64) (string, error) {
	var encrypted string
	err := s.db.QueryRow(`SELECT key_encrypted FROM downstream_keys WHERE id = ?`, id).Scan(&encrypted)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("key %d not found", id)
		}
		return "", fmt.Errorf("query key: %w", err)
	}
	if encrypted == "" {
		return "", nil // 旧密钥，无法恢复
	}
	plain, err := Decrypt(encrypted, s.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("decrypt key: %w", err)
	}
	return plain, nil
}

// LookupKeyByHash 按 SHA-256 哈希获取下游 Key。
func (s *Store) LookupKeyByHash(hash string) (*DownstreamKey, error) {
	row := s.db.QueryRow(
		`SELECT id, key_hash, key_prefix, name, rpm_limit, max_concurrent, enabled, created_at, updated_at
		 FROM downstream_keys WHERE key_hash=?`, hash,
	)

	var dk DownstreamKey
	if err := row.Scan(&dk.ID, &dk.KeyHash, &dk.KeyPrefix, &dk.Name, &dk.RPMLimit, &dk.MaxConcurrent, &dk.Enabled, &dk.CreatedAt, &dk.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("key not found")
		}
		return nil, fmt.Errorf("scan downstream key: %w", err)
	}
	return &dk, nil
}

// LookupKeyByID 按 ID 获取下游 Key。
func (s *Store) LookupKeyByID(id int64) (*DownstreamKey, error) {
	row := s.db.QueryRow(
		`SELECT id, key_hash, key_prefix, name, rpm_limit, max_concurrent, enabled, created_at, updated_at
		 FROM downstream_keys WHERE id=?`, id,
	)
	var dk DownstreamKey
	if err := row.Scan(&dk.ID, &dk.KeyHash, &dk.KeyPrefix, &dk.Name, &dk.RPMLimit, &dk.MaxConcurrent, &dk.Enabled, &dk.CreatedAt, &dk.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("key %d not found", id)
		}
		return nil, fmt.Errorf("scan downstream key: %w", err)
	}
	return &dk, nil
}

// ListKeys 按创建时间返回所有下游 Key。
func (s *Store) ListKeys() ([]DownstreamKey, error) {
	return s.queryKeys(
		`SELECT id, key_hash, key_prefix, name, rpm_limit, max_concurrent, enabled, created_at, updated_at
		 FROM downstream_keys ORDER BY created_at DESC`,
	)
}

// GetAllKeys 返回所有下游 Key（等价于 ListKeys；用于快照加载）。
func (s *Store) GetAllKeys() ([]DownstreamKey, error) {
	return s.ListKeys()
}

// UpdateKey 更新下游 Key 的可变字段。maxConcurrent 为 nil 时不修改并发限制。
func (s *Store) UpdateKey(id int64, name string, rpmLimit int, enabled bool, maxConcurrent *int) (*DownstreamKey, error) {
	now := time.Now().UTC()

	query := `UPDATE downstream_keys SET name=?, rpm_limit=?, enabled=?, updated_at=?`
	args := []interface{}{name, rpmLimit, enabled, now}

	if maxConcurrent != nil {
		query += `, max_concurrent=?`
		args = append(args, *maxConcurrent)
	}

	query += ` WHERE id=?`
	args = append(args, id)

	res, err := s.db.Exec(query, args...)
	if err != nil {
		return nil, fmt.Errorf("update downstream key: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return nil, fmt.Errorf("key %d not found", id)
	}

	row := s.db.QueryRow(
		`SELECT id, key_hash, key_prefix, name, rpm_limit, max_concurrent, enabled, created_at, updated_at
		 FROM downstream_keys WHERE id=?`, id,
	)
	var dk DownstreamKey
	if err := row.Scan(&dk.ID, &dk.KeyHash, &dk.KeyPrefix, &dk.Name, &dk.RPMLimit, &dk.MaxConcurrent, &dk.Enabled, &dk.CreatedAt, &dk.UpdatedAt); err != nil {
		return nil, fmt.Errorf("scan updated key: %w", err)
	}
	return &dk, nil
}

// DeleteKey 按 ID 删除下游 Key。
func (s *Store) DeleteKey(id int64) error {
	res, err := s.db.Exec(`DELETE FROM downstream_keys WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete downstream key: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("key %d not found", id)
	}
	return nil
}

// queryKeys 是一个辅助函数，执行 SELECT 查询并扫描 DownstreamKey 行。
func (s *Store) queryKeys(query string, args ...interface{}) ([]DownstreamKey, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query downstream keys: %w", err)
	}
	defer rows.Close()

	var result []DownstreamKey
	for rows.Next() {
		var dk DownstreamKey
		if err := rows.Scan(&dk.ID, &dk.KeyHash, &dk.KeyPrefix, &dk.Name, &dk.RPMLimit, &dk.MaxConcurrent, &dk.Enabled, &dk.CreatedAt, &dk.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan downstream key row: %w", err)
		}
		result = append(result, dk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate downstream key rows: %w", err)
	}
	return result, nil
}

// CountKeys 返回当前下游 Key 总数。
// 单独提供聚合查询，让状态接口拿统计时不必扫描完整 Key 列表。
func (s *Store) CountKeys() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM downstream_keys`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count keys: %w", err)
	}
	return count, nil
}
