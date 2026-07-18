package store

import (
	"fmt"
	"strings"
	"time"
)

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
