package store

import (
	"fmt"
)

// GetFullRecordingConfig 读取全量记录开关、范围模式和 Key 选择。
func (s *Store) GetFullRecordingConfig() (FullRecordingConfig, error) {
	var config FullRecordingConfig
	if err := s.db.QueryRow(`SELECT enabled, record_all_keys FROM full_recording_config WHERE id = 1`).Scan(&config.Enabled, &config.AllKeys); err != nil {
		return config, fmt.Errorf("read full recording config: %w", err)
	}
	rows, err := s.db.Query(`SELECT downstream_key_id FROM full_recording_keys ORDER BY downstream_key_id`)
	if err != nil {
		return config, fmt.Errorf("query full recording keys: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return config, fmt.Errorf("scan full recording key: %w", err)
		}
		config.DownstreamKeyIDs = append(config.DownstreamKeyIDs, id)
	}
	if err := rows.Err(); err != nil {
		return config, fmt.Errorf("iterate full recording keys: %w", err)
	}
	return config, nil
}

// SetFullRecordingConfig 在单个事务中更新开关并替换 Key 选择。
func (s *Store) SetFullRecordingConfig(config FullRecordingConfig) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin full recording config update: %w", err)
	}
	if _, err = tx.Exec(`UPDATE full_recording_config SET enabled = ?, record_all_keys = ? WHERE id = 1`, config.Enabled, config.AllKeys); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("update full recording switch: %w", err)
	}
	if _, err = tx.Exec(`DELETE FROM full_recording_keys`); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("clear full recording keys: %w", err)
	}
	seen := make(map[int64]struct{}, len(config.DownstreamKeyIDs))
	for _, id := range config.DownstreamKeyIDs {
		if config.AllKeys {
			break
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if _, err = tx.Exec(`INSERT INTO full_recording_keys (downstream_key_id) VALUES (?)`, id); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert full recording key %d: %w", id, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit full recording config update: %w", err)
	}
	return nil
}
