package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// 配置导出 / 导入
// ---------------------------------------------------------------------------

// ConfigExport 表示可序列化的系统配置快照（不含敏感信息）。
type ConfigExport struct {
	Version    string            `json:"version"`
	ExportedAt time.Time         `json:"exported_at"`
	Upstreams  []UpstreamExport  `json:"upstreams"`
	Keys       []KeyExport       `json:"keys"`
	Whitelist  []string          `json:"whitelist"`
	Settings   map[string]string `json:"settings"`
}

// UpstreamExport 表示导出的上游配置（不含 API Key）。
type UpstreamExport struct {
	Name              string   `json:"name"`
	BaseURL           string   `json:"base_url"`
	Priority          int      `json:"priority"`
	Enabled           bool     `json:"enabled"`
	ProxyURL          string   `json:"proxy_url"`
	KeySchedulingMode string   `json:"key_scheduling_mode"`
	AuthMode          string   `json:"auth_mode"`
	Remark            string   `json:"remark"`
	WebSocketEnabled  bool     `json:"websocket_enabled"`
	ModelPatterns     []string `json:"model_patterns"`
}

// KeyExport 表示导出的下游 Key 配置（不含明文/哈希等敏感字段）。
type KeyExport struct {
	Name          string `json:"name"`
	RPMLimit      int    `json:"rpm_limit"`
	Enabled       bool   `json:"enabled"`
	MaxConcurrent int    `json:"max_concurrent"`
}

// ExportConfig 导出系统配置快照，不含敏感信息（API Key、Key 明文/哈希）。
func (s *Store) ExportConfig() (*ConfigExport, error) {
	cfg := &ConfigExport{
		Version:    "1",
		ExportedAt: time.Now().UTC(),
		Settings:   make(map[string]string),
	}

	// 导出上游（不含 API Key）
	upstreams, err := s.ListUpstreams()
	if err != nil {
		return nil, fmt.Errorf("export upstreams: %w", err)
	}
	allPatterns, err := s.GetAllUpstreamModelPatterns()
	if err != nil {
		return nil, fmt.Errorf("export upstream model patterns: %w", err)
	}
	for _, u := range upstreams {
		cfg.Upstreams = append(cfg.Upstreams, UpstreamExport{
			Name:              u.Name,
			BaseURL:           u.BaseURL,
			Priority:          u.Priority,
			Enabled:           u.Enabled,
			ProxyURL:          u.ProxyURL,
			KeySchedulingMode: u.KeySchedulingMode,
			AuthMode:          u.AuthMode,
			Remark:            u.Remark,
			WebSocketEnabled:  u.WebSocketEnabled,
			ModelPatterns:     allPatterns[u.ID],
		})
	}

	// 导出下游 Key（不含明文/哈希）
	keys, err := s.ListKeys()
	if err != nil {
		return nil, fmt.Errorf("export keys: %w", err)
	}
	for _, k := range keys {
		cfg.Keys = append(cfg.Keys, KeyExport{
			Name:          k.Name,
			RPMLimit:      k.RPMLimit,
			Enabled:       k.Enabled,
			MaxConcurrent: k.MaxConcurrent,
		})
	}

	// 导出白名单
	wl, err := s.ListModelWhitelist()
	if err != nil {
		return nil, fmt.Errorf("export whitelist: %w", err)
	}
	for _, w := range wl {
		cfg.Whitelist = append(cfg.Whitelist, w.Pattern)
	}

	// 导出设置
	settingsRows, err := s.db.Query(`SELECT key, value FROM settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("export settings: %w", err)
	}
	defer settingsRows.Close()
	for settingsRows.Next() {
		var k, v string
		if err := settingsRows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan setting row: %w", err)
		}
		cfg.Settings[k] = v
	}
	if err := settingsRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate settings rows: %w", err)
	}

	return cfg, nil
}

// ImportConfig 从配置快照导入上游、Key、白名单和设置。
// 按名称跳过已存在的记录；不导入 API Key（安全考虑，需手动添加）。
func (s *Store) ImportConfig(cfg *ConfigExport) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin import transaction: %w", err)
	}

	now := time.Now().UTC()

	// 导入上游（按名称去重）
	for _, u := range cfg.Upstreams {
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM upstream_providers WHERE name = ?`, u.Name).Scan(&exists); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("check upstream exists %q: %w", u.Name, err)
		}
		if exists > 0 {
			continue // 跳过已存在的上游
		}

		authMode := u.AuthMode
		if authMode == "" {
			authMode = "api_key"
		}
		ksm := u.KeySchedulingMode
		if ksm == "" {
			ksm = "round-robin"
		}

		res, err := tx.Exec(
			`INSERT INTO upstream_providers (name, base_url, api_key, priority, enabled, proxy_url, key_scheduling_mode, auth_mode, remark, websocket_enabled, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			u.Name, u.BaseURL, "_migrated_to_upstream_api_keys", u.Priority, u.Enabled, u.ProxyURL, ksm, authMode, u.Remark, u.WebSocketEnabled, now, now,
		)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("import upstream %q: %w", u.Name, err)
		}

		// 导入模型模式
		if len(u.ModelPatterns) > 0 {
			upID, _ := res.LastInsertId()
			for _, p := range u.ModelPatterns {
				if _, err := tx.Exec(
					`INSERT INTO upstream_model_patterns (upstream_id, pattern, created_at) VALUES (?, ?, ?)`,
					upID, p, now,
				); err != nil {
					_ = tx.Rollback()
					return fmt.Errorf("import model pattern %q for upstream %q: %w", p, u.Name, err)
				}
			}
		}
	}

	// 导入下游 Key（按名称去重，不导入明文/哈希）
	for _, k := range cfg.Keys {
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM downstream_keys WHERE name = ?`, k.Name).Scan(&exists); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("check key exists %q: %w", k.Name, err)
		}
		if exists > 0 {
			continue // 跳过已存在的 Key
		}

		// 生成新的 Key
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("generate key bytes: %w", err)
		}
		plaintext := "sk-" + hex.EncodeToString(raw)
		prefix := plaintext[:len("sk-")+8]
		hashBytes := sha256.Sum256([]byte(plaintext))
		keyHash := hex.EncodeToString(hashBytes[:])
		encrypted, err := Encrypt(plaintext, s.encryptionKey)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("encrypt imported key: %w", err)
		}

		if _, err := tx.Exec(
			`INSERT INTO downstream_keys (key_hash, key_prefix, name, rpm_limit, max_concurrent, enabled, key_encrypted, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			keyHash, prefix, k.Name, k.RPMLimit, k.MaxConcurrent, k.Enabled, encrypted, now, now,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("import key %q: %w", k.Name, err)
		}
	}

	// 导入白名单（按 pattern 去重）
	for _, pattern := range cfg.Whitelist {
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM model_whitelist WHERE pattern = ?`, pattern).Scan(&exists); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("check whitelist exists %q: %w", pattern, err)
		}
		if exists > 0 {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO model_whitelist (pattern, created_at) VALUES (?, ?)`,
			pattern, now,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("import whitelist pattern %q: %w", pattern, err)
		}
	}

	// 导入设置（仅允许已知的配置项，防止注入任意键）
	allowedSettings := map[string]bool{
		"auto_disable_threshold":    true,
		"log_retention_days":        true,
		"slow_request_threshold_ms": true,
	}
	for k, v := range cfg.Settings {
		if !allowedSettings[k] {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			k, v,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("import setting %q: %w", k, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit config import: %w", err)
	}
	return nil
}
