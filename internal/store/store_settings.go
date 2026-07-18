package store

import (
	"database/sql"
	"fmt"
)

// GetSetting 从 settings 表读取配置项，不存在时返回 defaultValue。
func (s *Store) GetSetting(key, defaultValue string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return defaultValue, nil
	}
	if err != nil {
		return defaultValue, err
	}
	return value, nil
}

// SetSetting 写入或更新 settings 表中的配置项。
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
}
