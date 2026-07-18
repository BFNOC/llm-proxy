package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// getUpstreamAPIKeys 返回单个上游的所有已启用 API Key（已解密）。
// 仅供代理运行时使用；禁用的 Key 不会被加载到内存。
func (s *Store) getUpstreamAPIKeys(upstreamID int64) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT api_key FROM upstream_api_keys WHERE upstream_id = ? AND enabled = 1 ORDER BY id`, upstreamID,
	)
	if err != nil {
		return nil, fmt.Errorf("query upstream api keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var encrypted string
		if err := rows.Scan(&encrypted); err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		plain, err := Decrypt(encrypted, s.encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt api key for upstream %d: %w", upstreamID, err)
		}
		keys = append(keys, plain)
	}
	return keys, rows.Err()
}

// getAllUpstreamAPIKeys 一次性加载所有上游的已启用 API Key（已解密），供 ListUpstreams 批量填充。
func (s *Store) getAllUpstreamAPIKeys() (map[int64][]string, error) {
	rows, err := s.db.Query(`SELECT upstream_id, api_key FROM upstream_api_keys WHERE enabled = 1 ORDER BY upstream_id, id`)
	if err != nil {
		return nil, fmt.Errorf("query all upstream api keys: %w", err)
	}
	defer rows.Close()

	result := make(map[int64][]string)
	for rows.Next() {
		var upstreamID int64
		var encrypted string
		if err := rows.Scan(&upstreamID, &encrypted); err != nil {
			return nil, fmt.Errorf("scan api key row: %w", err)
		}
		plain, err := Decrypt(encrypted, s.encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt api key for upstream %d: %w", upstreamID, err)
		}
		result[upstreamID] = append(result[upstreamID], plain)
	}
	return result, rows.Err()
}

// GetUpstreamAllAPIKeys 返回单个上游的所有 API Key（含启用状态和 row ID），供管理面板展示。
func (s *Store) GetUpstreamAllAPIKeys(upstreamID int64) ([]APIKeyInfo, error) {
	rows, err := s.db.Query(
		`SELECT id, api_key, enabled, consecutive_failures FROM upstream_api_keys WHERE upstream_id = ? ORDER BY id`, upstreamID,
	)
	if err != nil {
		return nil, fmt.Errorf("query upstream api keys: %w", err)
	}
	defer rows.Close()

	var result []APIKeyInfo
	for rows.Next() {
		var rowID int64
		var encrypted string
		var enabled bool
		var consecFails int
		if err := rows.Scan(&rowID, &encrypted, &enabled, &consecFails); err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		plain, err := Decrypt(encrypted, s.encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt api key for upstream %d: %w", upstreamID, err)
		}
		result = append(result, APIKeyInfo{RowID: rowID, Key: plain, Enabled: enabled, ConsecutiveFails: consecFails})
	}
	return result, rows.Err()
}

// SetAPIKeyEnabled 启用或禁用某个上游的指定 API Key（按 row ID）。
// 手动启用时同步清零 consecutive_failures，避免随后 prober 的
// AutoDisableFailingKeys 因历史失败计数立刻再次禁用（表现为「启用要点两次」）。
func (s *Store) SetAPIKeyEnabled(upstreamID, keyRowID int64, enabled bool) error {
	var res sql.Result
	var err error
	if enabled {
		res, err = s.db.Exec(
			`UPDATE upstream_api_keys SET enabled = 1, consecutive_failures = 0 WHERE id = ? AND upstream_id = ?`,
			keyRowID, upstreamID,
		)
	} else {
		res, err = s.db.Exec(
			`UPDATE upstream_api_keys SET enabled = 0 WHERE id = ? AND upstream_id = ?`,
			keyRowID, upstreamID,
		)
	}
	if err != nil {
		return fmt.Errorf("update api key enabled: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("api key %d not found for upstream %d", keyRowID, upstreamID)
	}
	return nil
}

// AddUpstreamAPIKeys 为指定上游追加 API Key，并返回追加后的完整 Key 列表。
func (s *Store) AddUpstreamAPIKeys(upstreamID int64, apiKeys []string) ([]APIKeyInfo, error) {
	if len(apiKeys) == 0 {
		return s.GetUpstreamAllAPIKeys(upstreamID)
	}

	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}

	var exists int
	if err := tx.QueryRow(`SELECT 1 FROM upstream_providers WHERE id = ?`, upstreamID).Scan(&exists); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("upstream %d not found", upstreamID)
		}
		return nil, fmt.Errorf("check upstream exists: %w", err)
	}

	stmt, err := tx.Prepare(`INSERT INTO upstream_api_keys (upstream_id, api_key, created_at) VALUES (?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("prepare api key insert: %w", err)
	}
	defer stmt.Close()

	for _, key := range apiKeys {
		encrypted, err := Encrypt(key, s.encryptionKey)
		if err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("encrypt api key: %w", err)
		}
		if _, err = stmt.Exec(upstreamID, encrypted, now); err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("insert api key: %w", err)
		}
	}
	if _, err = tx.Exec(`UPDATE upstream_providers SET updated_at = ? WHERE id = ?`, now, upstreamID); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("touch upstream: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit api key append: %w", err)
	}
	return s.GetUpstreamAllAPIKeys(upstreamID)
}

// DeleteUpstreamAPIKey 删除指定上游的单个 API Key。
func (s *Store) DeleteUpstreamAPIKey(upstreamID, keyRowID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM upstream_api_keys WHERE id = ? AND upstream_id = ?`, keyRowID, upstreamID)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete api key: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		_ = tx.Rollback()
		return fmt.Errorf("api key %d not found for upstream %d", keyRowID, upstreamID)
	}
	if _, err = tx.Exec(`UPDATE upstream_providers SET updated_at = ? WHERE id = ?`, time.Now().UTC(), upstreamID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("touch upstream: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit api key delete: %w", err)
	}
	return nil
}

// IncrKeyFailures 增加指定 API Key 的连续失败次数，并在达到阈值时立即禁用。
// 返回新的连续失败次数。
func (s *Store) IncrKeyFailures(upstreamID, keyRowID int64, threshold int) (int, error) {
	_, err := s.db.Exec(
		`UPDATE upstream_api_keys SET consecutive_failures = consecutive_failures + 1 WHERE id = ? AND upstream_id = ?`,
		keyRowID, upstreamID,
	)
	if err != nil {
		return 0, err
	}
	var count int
	if err := s.db.QueryRow(
		`SELECT consecutive_failures FROM upstream_api_keys WHERE id = ? AND upstream_id = ?`,
		keyRowID, upstreamID,
	).Scan(&count); err != nil {
		return 0, err
	}
	if threshold > 0 && count >= threshold {
		if _, err := s.db.Exec(
			`UPDATE upstream_api_keys SET enabled = 0 WHERE id = ? AND upstream_id = ?`,
			keyRowID, upstreamID,
		); err != nil {
			return count, err
		}
	}
	return count, nil
}

// ResetKeyFailures 重置指定 API Key 的连续失败次数为 0。
func (s *Store) ResetKeyFailures(upstreamID, keyRowID int64) error {
	_, err := s.db.Exec(
		`UPDATE upstream_api_keys SET consecutive_failures = 0 WHERE id = ? AND upstream_id = ?`,
		keyRowID, upstreamID,
	)
	if err != nil {
		return fmt.Errorf("reset key failures (upstream=%d, key=%d): %w", upstreamID, keyRowID, err)
	}
	return nil
}

// AutoDisableFailingKeys 将连续失败次数 >= threshold 的 Key 自动禁用。
func (s *Store) AutoDisableFailingKeys(threshold int) (int64, error) {
	res, err := s.db.Exec(
		`UPDATE upstream_api_keys SET enabled = 0 WHERE consecutive_failures >= ? AND enabled = 1`,
		threshold,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// GetAllUpstreamAPIKeyRowIDs 一次性加载所有上游的已启用 Key 行 ID，供 prober 构建运行时快照。
func (s *Store) GetAllUpstreamAPIKeyRowIDs() (map[int64][]int64, error) {
	rows, err := s.db.Query(`SELECT upstream_id, id FROM upstream_api_keys WHERE enabled = 1 ORDER BY upstream_id, id`)
	if err != nil {
		return nil, fmt.Errorf("query all upstream api key row ids: %w", err)
	}
	defer rows.Close()

	result := make(map[int64][]int64)
	for rows.Next() {
		var upstreamID, rowID int64
		if err := rows.Scan(&upstreamID, &rowID); err != nil {
			return nil, fmt.Errorf("scan api key row id: %w", err)
		}
		result[upstreamID] = append(result[upstreamID], rowID)
	}
	return result, rows.Err()
}
