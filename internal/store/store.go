package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store 提供对 SQLite 数据库的访问。
type Store struct {
	db            *sql.DB
	encryptionKey []byte
}

// NewStore 打开 dbPath 处的 SQLite 数据库，应用 PRAGMA 设置并运行迁移。
// encryptionKey 必须正好是 32 字节。
func NewStore(dbPath string, encryptionKey []byte) (*Store, error) {
	if len(encryptionKey) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(encryptionKey))
	}

	// 使用 DSN 参数确保 PRAGMA 设置应用到连接池中的每个连接。
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// 单连接通过一个 WAL 快照序列化所有访问，
	// 防止写后读场景中出现脏读竞争。
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err = RunMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &Store{db: db, encryptionKey: encryptionKey}, nil
}

// Close 关闭底层数据库连接。
func (s *Store) Close() error {
	return s.db.Close()
}

// ---------------------------------------------------------------------------
// 上游 CRUD
// ---------------------------------------------------------------------------

// CreateUpstream 插入一个新的上游 provider，可携带一个或多个 API Key。
// 每个 Key 在写入 upstream_api_keys 表之前都会被加密。
// URL 校验（scheme、SSRF）由 HTTP handler 层负责；
// store 层接受任意非空 URL，以便使用 loopback 地址进行测试。
func (s *Store) CreateUpstream(name, baseURL string, apiKeys []string, priority int, proxyURL string, keySchedulingMode string, authMode string, remark string, websocketEnabled bool, autoDiscoverModels bool, upstreamRPMLimit int) (*UpstreamProvider, error) {
	if keySchedulingMode == "" {
		keySchedulingMode = "round-robin"
	}
	if authMode == "" {
		authMode = "api_key"
	}

	now := time.Now().UTC()

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}

	// 旧 api_key 列保留占位值（NOT NULL 约束无法删除）
	res, err := tx.Exec(
		`INSERT INTO upstream_providers (name, base_url, api_key, priority, enabled, proxy_url, key_scheduling_mode, auth_mode, remark, websocket_enabled, auto_discover_models, upstream_rpm_limit, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		name, baseURL, "_migrated_to_upstream_api_keys", priority, proxyURL, keySchedulingMode, authMode, remark, websocketEnabled, autoDiscoverModels, upstreamRPMLimit, now, now,
	)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("insert upstream: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("last insert id: %w", err)
	}

	// 将所有 Key 加密写入 upstream_api_keys 表
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
		if _, err = stmt.Exec(id, encrypted, now); err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("insert api key: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit upstream creation: %w", err)
	}

	return &UpstreamProvider{
		ID:                 id,
		Name:               name,
		BaseURL:            baseURL,
		APIKeys:            apiKeys,
		ProxyURL:           proxyURL,
		Priority:           priority,
		Enabled:            true,
		KeySchedulingMode:  keySchedulingMode,
		AuthMode:           authMode,
		Remark:             remark,
		WebSocketEnabled:   websocketEnabled,
		AutoDiscoverModels: autoDiscoverModels,
		UpstreamRPMLimit:   upstreamRPMLimit,
		Healthy:            true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

// GetUpstream 按 ID 获取一个上游 provider，并解密其所有 API Key。
func (s *Store) GetUpstream(id int64) (*UpstreamProvider, error) {
	row := s.db.QueryRow(
		`SELECT id, name, base_url, priority, enabled, key_scheduling_mode, auth_mode, remark, websocket_enabled, auto_discover_models, last_model_discovery, proxy_url, upstream_rpm_limit, circuit_breaker_threshold, circuit_breaker_recovery_seconds, deleted_at, created_at, updated_at
		 FROM upstream_providers WHERE id = ?`, id,
	)

	var up UpstreamProvider
	var lastModelDiscovery sql.NullTime
	var deletedAt sql.NullTime
	if err := row.Scan(&up.ID, &up.Name, &up.BaseURL, &up.Priority, &up.Enabled, &up.KeySchedulingMode, &up.AuthMode, &up.Remark, &up.WebSocketEnabled, &up.AutoDiscoverModels, &lastModelDiscovery, &up.ProxyURL, &up.UpstreamRPMLimit, &up.CircuitBreakerThreshold, &up.CircuitBreakerRecoverySeconds, &deletedAt, &up.CreatedAt, &up.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("upstream %d not found", id)
		}
		return nil, fmt.Errorf("scan upstream: %w", err)
	}
	if lastModelDiscovery.Valid {
		t := lastModelDiscovery.Time
		up.LastModelDiscovery = &t
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		up.DeletedAt = &t
	}

	keys, err := s.getUpstreamAPIKeys(id)
	if err != nil {
		return nil, fmt.Errorf("get upstream api keys for %d: %w", id, err)
	}
	up.APIKeys = keys
	up.Healthy = true
	return &up, nil
}

// ListUpstreams 返回所有上游 provider，并附带已解密的 API Key。
func (s *Store) ListUpstreams() ([]UpstreamProvider, error) {
	rows, err := s.db.Query(
		`SELECT id, name, base_url, priority, enabled, key_scheduling_mode, auth_mode, remark, websocket_enabled, auto_discover_models, last_model_discovery, proxy_url, upstream_rpm_limit, circuit_breaker_threshold, circuit_breaker_recovery_seconds, deleted_at, created_at, updated_at
		 FROM upstream_providers WHERE deleted_at IS NULL ORDER BY priority ASC, id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query upstreams: %w", err)
	}
	defer rows.Close()

	var result []UpstreamProvider
	for rows.Next() {
		var up UpstreamProvider
		var lastModelDiscovery sql.NullTime
		var deletedAt sql.NullTime
		if err := rows.Scan(&up.ID, &up.Name, &up.BaseURL, &up.Priority, &up.Enabled, &up.KeySchedulingMode, &up.AuthMode, &up.Remark, &up.WebSocketEnabled, &up.AutoDiscoverModels, &lastModelDiscovery, &up.ProxyURL, &up.UpstreamRPMLimit, &up.CircuitBreakerThreshold, &up.CircuitBreakerRecoverySeconds, &deletedAt, &up.CreatedAt, &up.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan upstream row: %w", err)
		}
		if lastModelDiscovery.Valid {
			t := lastModelDiscovery.Time
			up.LastModelDiscovery = &t
		}
		if deletedAt.Valid {
			t := deletedAt.Time
			up.DeletedAt = &t
		}
		up.Healthy = true
		result = append(result, up)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upstream rows: %w", err)
	}

	// 批量加载所有上游的 API Keys，避免 N+1 查询
	allKeys, err := s.getAllUpstreamAPIKeys()
	if err != nil {
		return nil, fmt.Errorf("get all upstream api keys: %w", err)
	}
	for i := range result {
		result[i].APIKeys = allKeys[result[i].ID]
	}

	return result, nil
}

// UpdateUpstream 替换一个上游 provider 的所有可变字段。
// 如果 apiKeys 非 nil，则全量替换该上游的 API Key。
func (s *Store) UpdateUpstream(id int64, name, baseURL string, apiKeys []string, priority int, enabled bool, proxyURL string, keySchedulingMode string, authMode string, remark string, websocketEnabled *bool) (*UpstreamProvider, error) {
	now := time.Now().UTC()
	if authMode == "" {
		authMode = "api_key"
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}

	// 基础 UPDATE 字段
	query := `UPDATE upstream_providers SET name=?, base_url=?, priority=?, enabled=?, proxy_url=?, key_scheduling_mode=?, auth_mode=?, remark=?, updated_at=?`
	args := []interface{}{name, baseURL, priority, enabled, proxyURL, keySchedulingMode, authMode, remark, now}

	// websocketEnabled 为 nil 时不修改该列
	if websocketEnabled != nil {
		query += `, websocket_enabled=?`
		args = append(args, *websocketEnabled)
	}

	query += ` WHERE id=?`
	args = append(args, id)

	res, err := tx.Exec(query, args...)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("update upstream: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		_ = tx.Rollback()
		return nil, fmt.Errorf("upstream %d not found", id)
	}

	// 如果提供了新的 Key 列表，全量替换
	if apiKeys != nil {
		if _, err = tx.Exec(`DELETE FROM upstream_api_keys WHERE upstream_id = ?`, id); err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("clear existing api keys: %w", err)
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
			if _, err = stmt.Exec(id, encrypted, now); err != nil {
				_ = tx.Rollback()
				return nil, fmt.Errorf("insert api key: %w", err)
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit upstream update: %w", err)
	}

	return s.GetUpstream(id)
}

// SetWebSocketEnabled 仅更新指定上游的 websocket_enabled 列，
// 避免全字段 UpdateUpstream 在并发修改时覆盖其他字段。
func (s *Store) SetWebSocketEnabled(id int64, enabled bool) error {
	res, err := s.db.Exec(
		`UPDATE upstream_providers SET websocket_enabled=?, updated_at=? WHERE id=?`,
		enabled, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("set websocket_enabled: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("upstream %d not found", id)
	}
	return nil
}

// SetAutoDiscoverModels 仅更新指定上游的 auto_discover_models 列，
// 避免全字段 UpdateUpstream 在并发修改时覆盖其他字段。
func (s *Store) SetAutoDiscoverModels(id int64, enabled bool) error {
	res, err := s.db.Exec(
		`UPDATE upstream_providers SET auto_discover_models=?, updated_at=? WHERE id=?`,
		enabled, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("set auto_discover_models: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("upstream %d not found", id)
	}
	return nil
}

// UpdateDiscoveredModels 更新上游的模型模式列表并记录发现时间。
// 内部调用 SetUpstreamModelPatterns 全量覆盖模式，并更新 last_model_discovery 时间戳。
func (s *Store) UpdateDiscoveredModels(upstreamID int64, patterns []string) error {
	if err := s.SetUpstreamModelPatterns(upstreamID, patterns); err != nil {
		return fmt.Errorf("set upstream model patterns: %w", err)
	}
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE upstream_providers SET last_model_discovery=?, updated_at=? WHERE id=?`,
		now, now, upstreamID,
	)
	if err != nil {
		return fmt.Errorf("update last_model_discovery: %w", err)
	}
	return nil
}

// ReorderUpstreams 按传入的 ID 顺序重新设置上游优先级（位置 0 = 最高优先级）。
// 在单个事务中逐一 UPDATE，保证操作的原子性。
func (s *Store) ReorderUpstreams(ids []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	now := time.Now().UTC()
	for i, id := range ids {
		res, err := tx.Exec(
			`UPDATE upstream_providers SET priority=?, updated_at=? WHERE id=?`,
			i, now, id,
		)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("reorder upstream %d: %w", id, err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("rows affected for upstream %d: %w", id, err)
		}
		if affected == 0 {
			_ = tx.Rollback()
			return fmt.Errorf("upstream %d not found", id)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit reorder: %w", err)
	}
	return nil
}

// SetAllUpstreamsEnabled 批量设置所有上游的启用状态，返回受影响的行数。
func (s *Store) SetAllUpstreamsEnabled(enabled bool) (int64, error) {
	res, err := s.db.Exec(
		`UPDATE upstream_providers SET enabled=?, updated_at=? WHERE 1=1`,
		enabled, time.Now().UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("set all upstreams enabled: %w", err)
	}
	return res.RowsAffected()
}

// DeleteUpstream 软删除一个上游 provider（设置 deleted_at），不实际删除数据。
// ListUpstreams 默认排除软删除行；可通过 UndoDeleteUpstream 撤销。
func (s *Store) DeleteUpstream(id int64) error {
	return s.SoftDeleteUpstream(id)
}

// HardDeleteUpstream 按 ID 物理删除一个上游 provider。
// CASCADE 外键会自动删除 upstream_api_keys 中的关联 Key。
func (s *Store) HardDeleteUpstream(id int64) error {
	res, err := s.db.Exec(`DELETE FROM upstream_providers WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("hard delete upstream: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("upstream %d not found", id)
	}
	return nil
}

// SoftDeleteUpstream 设置上游的 deleted_at 为当前时间，实现软删除。
func (s *Store) SoftDeleteUpstream(id int64) error {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		`UPDATE upstream_providers SET deleted_at=?, updated_at=? WHERE id=? AND deleted_at IS NULL`,
		now, now, id,
	)
	if err != nil {
		return fmt.Errorf("soft delete upstream: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("upstream %d not found or already deleted", id)
	}
	return nil
}

// UndoDeleteUpstream 撤销软删除，将 deleted_at 置为 NULL。
func (s *Store) UndoDeleteUpstream(id int64) error {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		`UPDATE upstream_providers SET deleted_at=NULL, updated_at=? WHERE id=? AND deleted_at IS NOT NULL`,
		now, id,
	)
	if err != nil {
		return fmt.Errorf("undo delete upstream: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("upstream %d not found or not deleted", id)
	}
	return nil
}

// PurgeDeletedUpstreams 物理删除软删除时间超过 olderThan 的上游，返回已删除行数。
func (s *Store) PurgeDeletedUpstreams(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	res, err := s.db.Exec(
		`DELETE FROM upstream_providers WHERE deleted_at IS NOT NULL AND deleted_at < ?`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("purge deleted upstreams: %w", err)
	}
	return res.RowsAffected()
}

// ListDeletedUpstreams 返回所有已软删除的上游 provider。
func (s *Store) ListDeletedUpstreams() ([]UpstreamProvider, error) {
	rows, err := s.db.Query(
		`SELECT id, name, base_url, priority, enabled, key_scheduling_mode, auth_mode, remark, websocket_enabled, auto_discover_models, last_model_discovery, proxy_url, upstream_rpm_limit, circuit_breaker_threshold, circuit_breaker_recovery_seconds, deleted_at, created_at, updated_at
		 FROM upstream_providers WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query deleted upstreams: %w", err)
	}
	defer rows.Close()

	var result []UpstreamProvider
	for rows.Next() {
		var up UpstreamProvider
		var lastModelDiscovery sql.NullTime
		var deletedAt sql.NullTime
		if err := rows.Scan(&up.ID, &up.Name, &up.BaseURL, &up.Priority, &up.Enabled, &up.KeySchedulingMode, &up.AuthMode, &up.Remark, &up.WebSocketEnabled, &up.AutoDiscoverModels, &lastModelDiscovery, &up.ProxyURL, &up.UpstreamRPMLimit, &up.CircuitBreakerThreshold, &up.CircuitBreakerRecoverySeconds, &deletedAt, &up.CreatedAt, &up.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan deleted upstream row: %w", err)
		}
		if lastModelDiscovery.Valid {
			t := lastModelDiscovery.Time
			up.LastModelDiscovery = &t
		}
		if deletedAt.Valid {
			t := deletedAt.Time
			up.DeletedAt = &t
		}
		result = append(result, up)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deleted upstream rows: %w", err)
	}

	// 批量加载所有上游的 API Keys
	allKeys, err := s.getAllUpstreamAPIKeys()
	if err != nil {
		return nil, fmt.Errorf("get all upstream api keys: %w", err)
	}
	for i := range result {
		result[i].APIKeys = allKeys[result[i].ID]
	}

	return result, nil
}

// SetUpstreamRPMLimit 仅更新指定上游的 upstream_rpm_limit 列。
func (s *Store) SetUpstreamRPMLimit(id int64, limit int) error {
	res, err := s.db.Exec(
		`UPDATE upstream_providers SET upstream_rpm_limit=?, updated_at=? WHERE id=?`,
		limit, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("set upstream_rpm_limit: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("upstream %d not found", id)
	}
	return nil
}

// SetCircuitBreakerConfig 更新指定上游的熔断器配置。
func (s *Store) SetCircuitBreakerConfig(id int64, threshold, recoverySeconds int) error {
	res, err := s.db.Exec(
		`UPDATE upstream_providers SET circuit_breaker_threshold=?, circuit_breaker_recovery_seconds=?, updated_at=? WHERE id=?`,
		threshold, recoverySeconds, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("set circuit breaker config: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("upstream %d not found", id)
	}
	return nil
}

// UpsertUpstreamRateInfo 插入或更新上游速率信息（从响应头观测）。
func (s *Store) UpsertUpstreamRateInfo(info *UpstreamRateInfo) error {
	_, err := s.db.Exec(
		`INSERT INTO upstream_rate_info (upstream_id, rpm_limit, rpm_remaining, tpm_limit, tpm_remaining, reset_at, last_429_at, consecutive_429s, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(upstream_id) DO UPDATE SET
		   rpm_limit=excluded.rpm_limit, rpm_remaining=excluded.rpm_remaining,
		   tpm_limit=excluded.tpm_limit, tpm_remaining=excluded.tpm_remaining,
		   reset_at=excluded.reset_at, last_429_at=excluded.last_429_at,
		   consecutive_429s=excluded.consecutive_429s, updated_at=excluded.updated_at`,
		info.UpstreamID, info.RPMLimit, info.RPMRemaining, info.TPMLimit, info.TPMRemaining,
		info.ResetAt, info.Last429At, info.Consecutive429s, info.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert upstream rate info (upstream=%d): %w", info.UpstreamID, err)
	}
	return nil
}

// GetUpstreamRateInfo 返回单个上游的速率信息。
func (s *Store) GetUpstreamRateInfo(upstreamID int64) (*UpstreamRateInfo, error) {
	row := s.db.QueryRow(
		`SELECT upstream_id, rpm_limit, rpm_remaining, tpm_limit, tpm_remaining, reset_at, last_429_at, consecutive_429s, updated_at
		 FROM upstream_rate_info WHERE upstream_id = ?`, upstreamID,
	)
	var info UpstreamRateInfo
	var resetAt, last429At sql.NullTime
	if err := row.Scan(&info.UpstreamID, &info.RPMLimit, &info.RPMRemaining, &info.TPMLimit, &info.TPMRemaining, &resetAt, &last429At, &info.Consecutive429s, &info.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("upstream rate info for %d not found", upstreamID)
		}
		return nil, fmt.Errorf("scan upstream rate info: %w", err)
	}
	if resetAt.Valid {
		t := resetAt.Time
		info.ResetAt = &t
	}
	if last429At.Valid {
		t := last429At.Time
		info.Last429At = &t
	}
	return &info, nil
}

// GetAllUpstreamRateInfo 返回所有上游的速率信息。
func (s *Store) GetAllUpstreamRateInfo() ([]UpstreamRateInfo, error) {
	rows, err := s.db.Query(
		`SELECT upstream_id, rpm_limit, rpm_remaining, tpm_limit, tpm_remaining, reset_at, last_429_at, consecutive_429s, updated_at
		 FROM upstream_rate_info ORDER BY upstream_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("query all upstream rate info: %w", err)
	}
	defer rows.Close()

	var result []UpstreamRateInfo
	for rows.Next() {
		var info UpstreamRateInfo
		var resetAt, last429At sql.NullTime
		if err := rows.Scan(&info.UpstreamID, &info.RPMLimit, &info.RPMRemaining, &info.TPMLimit, &info.TPMRemaining, &resetAt, &last429At, &info.Consecutive429s, &info.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan upstream rate info row: %w", err)
		}
		if resetAt.Valid {
			t := resetAt.Time
			info.ResetAt = &t
		}
		if last429At.Valid {
			t := last429At.Time
			info.Last429At = &t
		}
		result = append(result, info)
	}
	return result, rows.Err()
}

// Record429 记录一次 429 事件：累加 consecutive_429s 并更新 last_429_at。
// 如果该上游尚无 rate info 记录则自动创建。
func (s *Store) Record429(upstreamID int64) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`INSERT INTO upstream_rate_info (upstream_id, rpm_limit, rpm_remaining, tpm_limit, tpm_remaining, consecutive_429s, last_429_at, updated_at)
		 VALUES (?, 0, 0, 0, 0, 1, ?, ?)
		 ON CONFLICT(upstream_id) DO UPDATE SET
		   consecutive_429s = consecutive_429s + 1,
		   last_429_at = excluded.last_429_at,
		   updated_at = excluded.updated_at`,
		upstreamID, now, now,
	)
	if err != nil {
		return fmt.Errorf("record 429 (upstream=%d): %w", upstreamID, err)
	}
	return nil
}

// Reset429Counter 在成功请求后重置 consecutive_429s 为 0。
func (s *Store) Reset429Counter(upstreamID int64) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE upstream_rate_info SET consecutive_429s = 0, updated_at = ? WHERE upstream_id = ?`,
		now, upstreamID,
	)
	if err != nil {
		return fmt.Errorf("reset 429 counter (upstream=%d): %w", upstreamID, err)
	}
	return nil
}

// GetUpstreamTemplates 返回预置的上游配置模板列表（硬编码，不存 DB）。
func GetUpstreamTemplates() []UpstreamTemplate {
	return []UpstreamTemplate{
		{Name: "OpenAI", BaseURL: "https://api.openai.com", AuthMode: "api_key", ModelPatterns: []string{"gpt-*", "o1-*", "o3-*", "dall-e-*", "text-embedding-*"}},
		{Name: "Anthropic", BaseURL: "https://api.anthropic.com", AuthMode: "api_key", ModelPatterns: []string{"claude-*"}},
		{Name: "Azure OpenAI", BaseURL: "https://{your-resource}.openai.azure.com", AuthMode: "api_key", ModelPatterns: []string{"gpt-*"}},
		{Name: "Groq", BaseURL: "https://api.groq.com/openai", AuthMode: "api_key", ModelPatterns: []string{"llama-*", "mixtral-*", "gemma-*"}},
		{Name: "Together AI", BaseURL: "https://api.together.xyz", AuthMode: "api_key", ModelPatterns: []string{"meta-llama/*", "mistralai/*"}},
		{Name: "Mistral", BaseURL: "https://api.mistral.ai", AuthMode: "api_key", ModelPatterns: []string{"mistral-*", "codestral-*", "pixtral-*"}},
		{Name: "DeepSeek", BaseURL: "https://api.deepseek.com", AuthMode: "api_key", ModelPatterns: []string{"deepseek-*"}},
		{Name: "xAI (Grok)", BaseURL: "https://api.x.ai", AuthMode: "api_key", ModelPatterns: []string{"grok-*"}},
	}
}

// BatchSetUpstreamEnabled 批量设置多个上游的启用状态，返回受影响的行数。
func (s *Store) BatchSetUpstreamEnabled(ids []int64, enabled bool) (int64, error) {
	ids = uniquePositiveIDs(ids)
	if len(ids) == 0 {
		return 0, nil
	}
	now := time.Now()
	placeholders, args := int64InClause(ids)
	args = append([]interface{}{enabled, now}, args...)
	query := `UPDATE upstream_providers SET enabled=?, updated_at=? WHERE id IN (` + placeholders + `)`
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, fmt.Errorf("batch set upstream enabled: %w", err)
	}
	return res.RowsAffected()
}

// BatchDeleteUpstreams 批量删除多个上游，CASCADE 会自动清理关联的 Key/绑定。
// 返回已删除的行数。
func (s *Store) BatchDeleteUpstreams(ids []int64) (int64, error) {
	ids = uniquePositiveIDs(ids)
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders, args := int64InClause(ids)
	query := `DELETE FROM upstream_providers WHERE id IN (` + placeholders + `)`
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, fmt.Errorf("batch delete upstreams: %w", err)
	}
	return res.RowsAffected()
}

// uniquePositiveIDs 去重并丢弃非正数 ID。
func uniquePositiveIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// int64InClause 为 IN 列表构建 "?,?,?" 占位符和参数。
func int64InClause(ids []int64) (placeholders string, args []interface{}) {
	parts := make([]string, len(ids))
	args = make([]interface{}, len(ids))
	for i, id := range ids {
		parts[i] = "?"
		args[i] = id
	}
	return strings.Join(parts, ","), args
}

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

// ---------------------------------------------------------------------------
// 请求日志
// ---------------------------------------------------------------------------

// InsertRequestLogBatch 在单个事务中批量插入请求日志。
func (s *Store) InsertRequestLogBatch(logs []RequestLog) error {
	if len(logs) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	stmt, err := tx.Prepare(
		`INSERT INTO request_logs (downstream_key_id, upstream_name, upstream_key_idx, model, used_proxy, client_ip, ip_region, provider_style, path, status_code, latency_ms, request_size, response_size, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, log := range logs {
		createdAt := log.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		if _, err = stmt.Exec(log.DownstreamKeyID, log.UpstreamName, log.UpstreamKeyIdx, log.Model, log.UsedProxy, log.ClientIP, log.IPRegion, log.ProviderStyle, log.Path, log.StatusCode, log.LatencyMs, log.RequestSize, log.ResponseSize, createdAt); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert request log: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit batch insert: %w", err)
	}
	return nil
}

// DeleteLogsOlderThan 删除早于给定时长（从现在算起）的请求日志。
func (s *Store) DeleteLogsOlderThan(d time.Duration) error {
	cutoff := time.Now().UTC().Add(-d)
	if _, err := s.db.Exec(`DELETE FROM request_logs WHERE created_at < ?`, cutoff); err != nil {
		return fmt.Errorf("delete old logs: %w", err)
	}
	return nil
}

// QueryLogs 获取指定 Key 在给定时间范围内的请求日志。
// keyID 传 0 表示查询所有 Key。limit<=0 表示不限制条数。
func (s *Store) QueryLogs(keyID int64, from, to time.Time, limit int) ([]RequestLog, error) {
	query := `SELECT id, downstream_key_id, upstream_name, upstream_key_idx, model, used_proxy, client_ip, ip_region, provider_style, path, status_code, latency_ms, request_size, response_size, created_at
	          FROM request_logs WHERE created_at >= ? AND created_at <= ?`
	args := []interface{}{from.UTC(), to.UTC()}

	if keyID != 0 {
		query += ` AND downstream_key_id = ?`
		args = append(args, keyID)
	}

	query += ` ORDER BY created_at DESC`

	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query request logs: %w", err)
	}
	defer rows.Close()

	var result []RequestLog
	for rows.Next() {
		var rl RequestLog
		if err := rows.Scan(&rl.ID, &rl.DownstreamKeyID, &rl.UpstreamName, &rl.UpstreamKeyIdx, &rl.Model, &rl.UsedProxy, &rl.ClientIP, &rl.IPRegion, &rl.ProviderStyle, &rl.Path, &rl.StatusCode, &rl.LatencyMs, &rl.RequestSize, &rl.ResponseSize, &rl.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan request log row: %w", err)
		}
		result = append(result, rl)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate request log rows: %w", err)
	}
	return result, nil
}

// GetLogByID 按 ID 获取单条请求日志，用于重放等场景。
func (s *Store) GetLogByID(id int64) (*RequestLog, error) {
	row := s.db.QueryRow(
		`SELECT id, downstream_key_id, upstream_name, upstream_key_idx, model, used_proxy, client_ip, ip_region, provider_style, path, status_code, latency_ms, request_size, response_size, created_at
		 FROM request_logs WHERE id = ?`, id,
	)
	var rl RequestLog
	if err := row.Scan(&rl.ID, &rl.DownstreamKeyID, &rl.UpstreamName, &rl.UpstreamKeyIdx, &rl.Model, &rl.UsedProxy, &rl.ClientIP, &rl.IPRegion, &rl.ProviderStyle, &rl.Path, &rl.StatusCode, &rl.LatencyMs, &rl.RequestSize, &rl.ResponseSize, &rl.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("log %d not found", id)
		}
		return nil, fmt.Errorf("scan log: %w", err)
	}
	return &rl, nil
}

// CountLogsSince 返回指定时间之后的请求日志条数。
// 管理状态页只需要聚合数字，直接走 COUNT(*) 可以避免把大量日志读入内存。
func (s *Store) CountLogsSince(since time.Time) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM request_logs WHERE created_at >= ?`, since.UTC()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count logs since: %w", err)
	}
	return count, nil
}

// KeyUsageStats 表示单个下游 Key 的使用统计。
type KeyUsageStats struct {
	KeyID        int64   `json:"key_id"`
	Total        int     `json:"total"`
	Success      int     `json:"success"`
	Error        int     `json:"error"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
}

// GetKeyUsageStats 按下游 Key 聚合请求日志统计。
func (s *Store) GetKeyUsageStats() ([]KeyUsageStats, error) {
	rows, err := s.db.Query(`
		SELECT downstream_key_id,
		       COUNT(*) as total,
		       SUM(CASE WHEN status_code < 400 THEN 1 ELSE 0 END) as success,
		       SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) as error,
		       AVG(latency_ms) as avg_latency
		FROM request_logs
		GROUP BY downstream_key_id
		ORDER BY total DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query key usage stats: %w", err)
	}
	defer rows.Close()
	var result []KeyUsageStats
	for rows.Next() {
		var s KeyUsageStats
		if err := rows.Scan(&s.KeyID, &s.Total, &s.Success, &s.Error, &s.AvgLatencyMs); err != nil {
			return nil, fmt.Errorf("scan key usage stats: %w", err)
		}
		result = append(result, s)
	}
	return result, rows.Err()
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

// ---- 模型白名单 ----

// ListModelWhitelist 返回所有白名单模式。
func (s *Store) ListModelWhitelist() ([]ModelWhitelistEntry, error) {
	rows, err := s.db.Query(`SELECT id, pattern, created_at FROM model_whitelist ORDER BY pattern`)
	if err != nil {
		return nil, fmt.Errorf("list model whitelist: %w", err)
	}
	defer rows.Close()

	var result []ModelWhitelistEntry
	for rows.Next() {
		var e ModelWhitelistEntry
		if err := rows.Scan(&e.ID, &e.Pattern, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan model whitelist: %w", err)
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// AddModelWhitelist 向白名单插入一个新模式。
func (s *Store) AddModelWhitelist(pattern string) (ModelWhitelistEntry, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		`INSERT INTO model_whitelist (pattern, created_at) VALUES (?, ?)`,
		pattern, now,
	)
	if err != nil {
		return ModelWhitelistEntry{}, fmt.Errorf("add model whitelist: %w", err)
	}
	id, _ := res.LastInsertId()
	return ModelWhitelistEntry{ID: id, Pattern: pattern, CreatedAt: now}, nil
}

// DeleteModelWhitelist 按 ID 删除一个模式。
func (s *Store) DeleteModelWhitelist(id int64) error {
	res, err := s.db.Exec(`DELETE FROM model_whitelist WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete model whitelist: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("model whitelist entry %d not found", id)
	}
	return nil
}

// BatchDeleteModelWhitelist 批量删除白名单条目。
func (s *Store) BatchDeleteModelWhitelist(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := `DELETE FROM model_whitelist WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, fmt.Errorf("batch delete model whitelist: %w", err)
	}
	return res.RowsAffected()
}

// ---------------------------------------------------------------------------
// Key ↔ 上游绑定关系
// ---------------------------------------------------------------------------

// SetKeyUpstreams 以“全量覆盖”方式更新某个下游 Key 的上游绑定。
// 先删后插放在同一事务中，读取方只会看到旧快照或新快照，
// 不会在更新过程中读到一半旧一半新的授权集合。
// 传入空切片表示清空显式绑定，回退到“该 Key 可使用所有健康上游”的默认语义。
func (s *Store) SetKeyUpstreams(keyID int64, upstreamIDs []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// 先清空旧绑定，保证接口语义是覆盖而不是增量追加。
	if _, err = tx.Exec(`DELETE FROM key_upstream_bindings WHERE downstream_key_id = ?`, keyID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("clear existing bindings: %w", err)
	}

	if len(upstreamIDs) > 0 {
		now := time.Now().UTC()
		// 复用 prepared statement，减少批量重建绑定时的语句解析开销。
		stmt, err := tx.Prepare(`INSERT INTO key_upstream_bindings (downstream_key_id, upstream_id, created_at) VALUES (?, ?, ?)`)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("prepare binding insert: %w", err)
		}
		defer stmt.Close()

		for _, uid := range upstreamIDs {
			if _, err = stmt.Exec(keyID, uid, now); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("insert binding (key=%d, upstream=%d): %w", keyID, uid, err)
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit bindings: %w", err)
	}
	return nil
}

// GetKeyUpstreamIDs 返回某个下游 Key 显式绑定的上游 ID 列表。
// 返回空切片表示“未配置绑定”，而不是“禁止访问任何上游”，
// 上层据此沿用历史默认行为，避免未配置即锁死。
func (s *Store) GetKeyUpstreamIDs(keyID int64) ([]int64, error) {
	rows, err := s.db.Query(
		// 固定排序让接口返回稳定，便于前端 diff、缓存命中和测试断言。
		`SELECT upstream_id FROM key_upstream_bindings WHERE downstream_key_id = ? ORDER BY upstream_id`,
		keyID,
	)
	if err != nil {
		return nil, fmt.Errorf("query key upstream bindings: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan upstream id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetAllKeyBindings 一次性加载所有显式绑定关系，供管理端列表页批量展示。
// 这样可以避免对每个 Key 再查一次绑定，减少典型的 N+1 查询问题。
func (s *Store) GetAllKeyBindings() (map[int64][]int64, error) {
	rows, err := s.db.Query(`SELECT downstream_key_id, upstream_id FROM key_upstream_bindings ORDER BY downstream_key_id, upstream_id`)
	if err != nil {
		return nil, fmt.Errorf("query all key bindings: %w", err)
	}
	defer rows.Close()

	result := make(map[int64][]int64)
	for rows.Next() {
		var keyID, upstreamID int64
		if err := rows.Scan(&keyID, &upstreamID); err != nil {
			return nil, fmt.Errorf("scan binding row: %w", err)
		}
		result[keyID] = append(result[keyID], upstreamID)
	}
	return result, rows.Err()
}

// ---------------------------------------------------------------------------
// 上游模型模式
// ---------------------------------------------------------------------------

// SetUpstreamModelPatterns 以全量覆盖方式更新某个上游的模型模式列表。
// 先删后插放在同一事务中，空切片表示清空模式（该上游接受所有模型）。
func (s *Store) SetUpstreamModelPatterns(upstreamID int64, patterns []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	if _, err = tx.Exec(`DELETE FROM upstream_model_patterns WHERE upstream_id = ?`, upstreamID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("clear existing patterns: %w", err)
	}

	if len(patterns) > 0 {
		now := time.Now().UTC()
		stmt, err := tx.Prepare(`INSERT INTO upstream_model_patterns (upstream_id, pattern, created_at) VALUES (?, ?, ?)`)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("prepare pattern insert: %w", err)
		}
		defer stmt.Close()

		for _, p := range patterns {
			if _, err = stmt.Exec(upstreamID, p, now); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("insert pattern (upstream=%d, pattern=%s): %w", upstreamID, p, err)
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit patterns: %w", err)
	}
	return nil
}

// GetUpstreamModelPatterns 返回单个上游的模型模式列表。
// 返回空切片表示"未配置模式，接受所有模型"。
func (s *Store) GetUpstreamModelPatterns(upstreamID int64) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT pattern FROM upstream_model_patterns WHERE upstream_id = ? ORDER BY pattern`,
		upstreamID,
	)
	if err != nil {
		return nil, fmt.Errorf("query upstream model patterns: %w", err)
	}
	defer rows.Close()

	var patterns []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan pattern: %w", err)
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}

// GetAllUpstreamModelPatterns 一次性加载所有上游的模型模式，供 prober 批量填充。
func (s *Store) GetAllUpstreamModelPatterns() (map[int64][]string, error) {
	rows, err := s.db.Query(`SELECT upstream_id, pattern FROM upstream_model_patterns ORDER BY upstream_id, pattern`)
	if err != nil {
		return nil, fmt.Errorf("query all upstream model patterns: %w", err)
	}
	defer rows.Close()

	result := make(map[int64][]string)
	for rows.Next() {
		var upstreamID int64
		var pattern string
		if err := rows.Scan(&upstreamID, &pattern); err != nil {
			return nil, fmt.Errorf("scan model pattern row: %w", err)
		}
		result[upstreamID] = append(result[upstreamID], pattern)
	}
	return result, rows.Err()
}

// ---------------------------------------------------------------------------
// 上游声明模型
// ---------------------------------------------------------------------------

// SetUpstreamDeclaredModels 以全量覆盖方式更新某个上游的声明模型列表。
// 先删后插放在同一事务中；空切片表示清空（该上游不声明任何模型）。
func (s *Store) SetUpstreamDeclaredModels(upstreamID int64, models []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	if _, err = tx.Exec(`DELETE FROM upstream_declared_models WHERE upstream_id = ?`, upstreamID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("clear existing declared models: %w", err)
	}

	if len(models) > 0 {
		now := time.Now().UTC()
		stmt, err := tx.Prepare(`INSERT INTO upstream_declared_models (upstream_id, model_id, created_at) VALUES (?, ?, ?)`)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("prepare declared model insert: %w", err)
		}
		defer stmt.Close()

		for _, m := range models {
			if _, err = stmt.Exec(upstreamID, m, now); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("insert declared model (upstream=%d, model=%s): %w", upstreamID, m, err)
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit declared models: %w", err)
	}
	return nil
}

// GetUpstreamDeclaredModels 返回单个上游的声明模型列表。
func (s *Store) GetUpstreamDeclaredModels(upstreamID int64) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT model_id FROM upstream_declared_models WHERE upstream_id = ? ORDER BY model_id`,
		upstreamID,
	)
	if err != nil {
		return nil, fmt.Errorf("query upstream declared models: %w", err)
	}
	defer rows.Close()

	var models []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, fmt.Errorf("scan declared model: %w", err)
		}
		models = append(models, m)
	}
	return models, rows.Err()
}

// GetAllUpstreamDeclaredModels 一次性加载所有已启用上游的声明模型，供 /v1/models 聚合。
func (s *Store) GetAllUpstreamDeclaredModels() (map[int64][]string, error) {
	rows, err := s.db.Query(`SELECT d.upstream_id, d.model_id FROM upstream_declared_models d JOIN upstream_providers u ON d.upstream_id = u.id WHERE u.enabled = 1 ORDER BY d.upstream_id, d.model_id`)
	if err != nil {
		return nil, fmt.Errorf("query all upstream declared models: %w", err)
	}
	defer rows.Close()

	result := make(map[int64][]string)
	for rows.Next() {
		var upstreamID int64
		var modelID string
		if err := rows.Scan(&upstreamID, &modelID); err != nil {
			return nil, fmt.Errorf("scan declared model row: %w", err)
		}
		result[upstreamID] = append(result[upstreamID], modelID)
	}
	return result, rows.Err()
}

// ---------------------------------------------------------------------------
// Key 模型路由覆盖
// ---------------------------------------------------------------------------

// KeyModelOverrideInput 是写入覆盖规则时使用的输入结构。
type KeyModelOverrideInput struct {
	ModelPattern string
	UpstreamID   int64
}

// SetKeyModelOverrides 以全量覆盖方式更新某个下游 Key 的模型路由覆盖。
// 先删后插放在同一事务中；空切片表示清空所有覆盖。
func (s *Store) SetKeyModelOverrides(keyID int64, overrides []KeyModelOverrideInput) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	if _, err = tx.Exec(`DELETE FROM key_model_overrides WHERE downstream_key_id = ?`, keyID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("clear existing overrides: %w", err)
	}

	if len(overrides) > 0 {
		now := time.Now().UTC()
		stmt, err := tx.Prepare(`INSERT INTO key_model_overrides (downstream_key_id, model_pattern, upstream_id, created_at) VALUES (?, ?, ?, ?)`)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("prepare override insert: %w", err)
		}
		defer stmt.Close()

		for _, o := range overrides {
			if _, err = stmt.Exec(keyID, o.ModelPattern, o.UpstreamID, now); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("insert override (key=%d, pattern=%s, upstream=%d): %w", keyID, o.ModelPattern, o.UpstreamID, err)
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit overrides: %w", err)
	}
	return nil
}

// GetKeyModelOverrides 返回某个下游 Key 的模型路由覆盖列表。
func (s *Store) GetKeyModelOverrides(keyID int64) ([]KeyModelOverride, error) {
	rows, err := s.db.Query(
		`SELECT id, downstream_key_id, model_pattern, upstream_id, created_at
		 FROM key_model_overrides WHERE downstream_key_id = ? ORDER BY model_pattern, upstream_id`,
		keyID,
	)
	if err != nil {
		return nil, fmt.Errorf("query key model overrides: %w", err)
	}
	defer rows.Close()

	var result []KeyModelOverride
	for rows.Next() {
		var o KeyModelOverride
		if err := rows.Scan(&o.ID, &o.DownstreamKeyID, &o.ModelPattern, &o.UpstreamID, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan key model override: %w", err)
		}
		result = append(result, o)
	}
	return result, rows.Err()
}

// GetAllKeyModelOverrides 一次性加载所有 Key 的模型路由覆盖，供缓存批量填充。
func (s *Store) GetAllKeyModelOverrides() (map[int64][]KeyModelOverride, error) {
	rows, err := s.db.Query(
		`SELECT id, downstream_key_id, model_pattern, upstream_id, created_at
		 FROM key_model_overrides ORDER BY downstream_key_id, model_pattern, upstream_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("query all key model overrides: %w", err)
	}
	defer rows.Close()

	result := make(map[int64][]KeyModelOverride)
	for rows.Next() {
		var o KeyModelOverride
		if err := rows.Scan(&o.ID, &o.DownstreamKeyID, &o.ModelPattern, &o.UpstreamID, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan key model override row: %w", err)
		}
		result[o.DownstreamKeyID] = append(result[o.DownstreamKeyID], o)
	}
	return result, rows.Err()
}

// ---------------------------------------------------------------------------
// 测试模型
// ---------------------------------------------------------------------------

// ListTestModels 返回所有测试模型，可选按协议过滤。
func (s *Store) ListTestModels(protocol string) ([]TestModel, error) {
	query := `SELECT id, name, protocol, created_at FROM test_models`
	var args []interface{}
	if protocol != "" {
		query += ` WHERE protocol = ?`
		args = append(args, protocol)
	}
	query += ` ORDER BY protocol, name`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query test models: %w", err)
	}
	defer rows.Close()
	var result []TestModel
	for rows.Next() {
		var m TestModel
		if err := rows.Scan(&m.ID, &m.Name, &m.Protocol, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan test model: %w", err)
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// CreateTestModel 插入一条测试模型记录。
func (s *Store) CreateTestModel(name, protocol string) (*TestModel, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		`INSERT INTO test_models (name, protocol, created_at) VALUES (?, ?, ?)`,
		name, protocol, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert test model: %w", err)
	}
	id, _ := res.LastInsertId()
	return &TestModel{ID: id, Name: name, Protocol: protocol, CreatedAt: now}, nil
}

// UpdateTestModel 更新测试模型的名称和协议。
func (s *Store) UpdateTestModel(id int64, name, protocol string) error {
	res, err := s.db.Exec(
		`UPDATE test_models SET name=?, protocol=? WHERE id=?`,
		name, protocol, id,
	)
	if err != nil {
		return fmt.Errorf("update test model: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("test model %d not found", id)
	}
	return nil
}

// DeleteTestModel 删除一条测试模型记录。
func (s *Store) DeleteTestModel(id int64) error {
	res, err := s.db.Exec(`DELETE FROM test_models WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete test model: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("test model %d not found", id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 上游健康探测历史
// ---------------------------------------------------------------------------

// RecordHealthProbe 记录一次上游健康探测结果。
func (s *Store) RecordHealthProbe(upstreamID int64, healthy bool, latencyMs int64, errorMsg string) error {
	_, err := s.db.Exec(
		`INSERT INTO upstream_health_history (upstream_id, healthy, latency_ms, error_message, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		upstreamID, healthy, latencyMs, errorMsg, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("record health probe (upstream=%d): %w", upstreamID, err)
	}
	return nil
}

// GetHealthHistory 返回指定上游最近 N 小时内的健康探测记录，按时间倒序，最多 limit 条。
func (s *Store) GetHealthHistory(upstreamID int64, hours int, limit int) ([]HealthRecord, error) {
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)

	query := `SELECT id, upstream_id, healthy, latency_ms, error_message, created_at
	          FROM upstream_health_history
	          WHERE upstream_id = ? AND created_at > ?
	          ORDER BY created_at DESC`
	args := []interface{}{upstreamID, since}

	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query health history (upstream=%d): %w", upstreamID, err)
	}
	defer rows.Close()

	var result []HealthRecord
	for rows.Next() {
		var r HealthRecord
		if err := rows.Scan(&r.ID, &r.UpstreamID, &r.Healthy, &r.LatencyMs, &r.ErrorMessage, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan health record: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// CleanHealthHistory 删除超过 retentionDays 天的健康探测记录。
func (s *Store) CleanHealthHistory(retentionDays int) error {
	if retentionDays < 1 {
		return fmt.Errorf("retentionDays must be >= 1, got %d", retentionDays)
	}
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	if _, err := s.db.Exec(`DELETE FROM upstream_health_history WHERE created_at < ?`, cutoff); err != nil {
		return fmt.Errorf("clean health history: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 上游延迟统计
// ---------------------------------------------------------------------------

// GetUpstreamLatencyStats 返回最近 N 小时内各上游的延迟统计（按 upstream_name 分组）。
func (s *Store) GetUpstreamLatencyStats(hours int) ([]map[string]interface{}, error) {
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	rows, err := s.db.Query(
		`SELECT upstream_name, COUNT(*) as total, AVG(latency_ms), MIN(latency_ms), MAX(latency_ms)
		 FROM request_logs WHERE created_at > ? GROUP BY upstream_name`,
		since,
	)
	if err != nil {
		return nil, fmt.Errorf("query upstream latency stats: %w", err)
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var name string
		var total int
		var avgLatency, minLatency, maxLatency float64
		if err := rows.Scan(&name, &total, &avgLatency, &minLatency, &maxLatency); err != nil {
			return nil, fmt.Errorf("scan latency stats row: %w", err)
		}
		result = append(result, map[string]interface{}{
			"upstream_name": name,
			"total":         total,
			"avg_latency":   avgLatency,
			"min_latency":   minLatency,
			"max_latency":   maxLatency,
		})
	}
	return result, rows.Err()
}

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
