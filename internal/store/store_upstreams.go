package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

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
