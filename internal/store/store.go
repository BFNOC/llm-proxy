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

// Store provides access to the SQLite database.
type Store struct {
	db            *sql.DB
	encryptionKey []byte
}

// NewStore opens the SQLite database at dbPath, applies PRAGMAs, and runs migrations.
// encryptionKey must be exactly 32 bytes.
func NewStore(dbPath string, encryptionKey []byte) (*Store, error) {
	if len(encryptionKey) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(encryptionKey))
	}

	// Use DSN parameters to ensure PRAGMAs apply to every connection in the pool.
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// Single-instance deployment: limit pool to 1 connection for SQLite safety.
	db.SetMaxOpenConns(1)

	if err = RunMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &Store{db: db, encryptionKey: encryptionKey}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// ---------------------------------------------------------------------------
// Upstream CRUD
// ---------------------------------------------------------------------------

// CreateUpstream inserts a new upstream provider. apiKey is encrypted before
// storage. URL validation (scheme, SSRF) is the responsibility of the HTTP
// handler layer; the store accepts any non-empty URL to remain testable with
// loopback addresses.
func (s *Store) CreateUpstream(name, baseURL, apiKey string, priority int, proxyURL string) (*UpstreamProvider, error) {
	encryptedKey, err := Encrypt(apiKey, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt api key: %w", err)
	}

	now := time.Now().UTC()
	res, err := s.db.Exec(
		`INSERT INTO upstream_providers (name, base_url, api_key, priority, enabled, proxy_url, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 1, ?, ?, ?)`,
		name, baseURL, encryptedKey, priority, proxyURL, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert upstream: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}

	return &UpstreamProvider{
		ID:        id,
		Name:      name,
		BaseURL:   baseURL,
		APIKey:    apiKey,
		ProxyURL:  proxyURL,
		Priority:  priority,
		Enabled:   true,
		Healthy:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// GetUpstream retrieves an upstream provider by ID, decrypting its API key.
func (s *Store) GetUpstream(id int64) (*UpstreamProvider, error) {
	row := s.db.QueryRow(
		`SELECT id, name, base_url, api_key, priority, enabled, proxy_url, created_at, updated_at
		 FROM upstream_providers WHERE id = ?`, id,
	)

	var up UpstreamProvider
	var encryptedKey string
	if err := row.Scan(&up.ID, &up.Name, &up.BaseURL, &encryptedKey, &up.Priority, &up.Enabled, &up.ProxyURL, &up.CreatedAt, &up.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("upstream %d not found", id)
		}
		return nil, fmt.Errorf("scan upstream: %w", err)
	}

	plainKey, err := Decrypt(encryptedKey, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt api key: %w", err)
	}
	up.APIKey = plainKey
	up.Healthy = true
	return &up, nil
}

// ListUpstreams returns all upstream providers with decrypted API keys.
func (s *Store) ListUpstreams() ([]UpstreamProvider, error) {
	rows, err := s.db.Query(
		`SELECT id, name, base_url, api_key, priority, enabled, proxy_url, created_at, updated_at
		 FROM upstream_providers ORDER BY priority ASC, id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query upstreams: %w", err)
	}
	defer rows.Close()

	var result []UpstreamProvider
	for rows.Next() {
		var up UpstreamProvider
		var encryptedKey string
		if err := rows.Scan(&up.ID, &up.Name, &up.BaseURL, &encryptedKey, &up.Priority, &up.Enabled, &up.ProxyURL, &up.CreatedAt, &up.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan upstream row: %w", err)
		}
		plainKey, err := Decrypt(encryptedKey, s.encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt api key for upstream %d: %w", up.ID, err)
		}
		up.APIKey = plainKey
		up.Healthy = true
		result = append(result, up)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upstream rows: %w", err)
	}
	return result, nil
}

// UpdateUpstream replaces all mutable fields of an upstream provider.
func (s *Store) UpdateUpstream(id int64, name, baseURL, apiKey string, priority int, enabled bool, proxyURL string) (*UpstreamProvider, error) {
	encryptedKey, err := Encrypt(apiKey, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt api key: %w", err)
	}

	now := time.Now().UTC()
	res, err := s.db.Exec(
		`UPDATE upstream_providers SET name=?, base_url=?, api_key=?, priority=?, enabled=?, proxy_url=?, updated_at=?
		 WHERE id=?`,
		name, baseURL, encryptedKey, priority, enabled, proxyURL, now, id,
	)
	if err != nil {
		return nil, fmt.Errorf("update upstream: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return nil, fmt.Errorf("upstream %d not found", id)
	}

	return s.GetUpstream(id)
}

// DeleteUpstream removes an upstream provider by ID.
func (s *Store) DeleteUpstream(id int64) error {
	res, err := s.db.Exec(`DELETE FROM upstream_providers WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete upstream: %w", err)
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

// ---------------------------------------------------------------------------
// Downstream Key CRUD
// ---------------------------------------------------------------------------

// CreateKey generates a new downstream API key, stores its SHA-256 hash, and
// returns the plaintext key exactly once.
func (s *Store) CreateKey(name string, rpmLimit int) (plaintext string, key *DownstreamKey, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate key bytes: %w", err)
	}

	plaintext = "sk-" + hex.EncodeToString(raw)
	prefix := plaintext[:len("sk-")+8] // "sk-" + first 8 hex chars

	hashBytes := sha256.Sum256([]byte(plaintext))
	keyHash := hex.EncodeToString(hashBytes[:])

	now := time.Now().UTC()
	res, err := s.db.Exec(
		`INSERT INTO downstream_keys (key_hash, key_prefix, name, rpm_limit, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 1, ?, ?)`,
		keyHash, prefix, name, rpmLimit, now, now,
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

// LookupKeyByHash retrieves a downstream key by its SHA-256 hash.
func (s *Store) LookupKeyByHash(hash string) (*DownstreamKey, error) {
	row := s.db.QueryRow(
		`SELECT id, key_hash, key_prefix, name, rpm_limit, enabled, created_at, updated_at
		 FROM downstream_keys WHERE key_hash=?`, hash,
	)

	var dk DownstreamKey
	if err := row.Scan(&dk.ID, &dk.KeyHash, &dk.KeyPrefix, &dk.Name, &dk.RPMLimit, &dk.Enabled, &dk.CreatedAt, &dk.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("key not found")
		}
		return nil, fmt.Errorf("scan downstream key: %w", err)
	}
	return &dk, nil
}

// LookupKeyByID retrieves a downstream key by its ID.
func (s *Store) LookupKeyByID(id int64) (*DownstreamKey, error) {
	row := s.db.QueryRow(
		`SELECT id, key_hash, key_prefix, name, rpm_limit, enabled, created_at, updated_at
		 FROM downstream_keys WHERE id=?`, id,
	)
	var dk DownstreamKey
	if err := row.Scan(&dk.ID, &dk.KeyHash, &dk.KeyPrefix, &dk.Name, &dk.RPMLimit, &dk.Enabled, &dk.CreatedAt, &dk.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("key %d not found", id)
		}
		return nil, fmt.Errorf("scan downstream key: %w", err)
	}
	return &dk, nil
}

// ListKeys returns all downstream keys ordered by creation time.
func (s *Store) ListKeys() ([]DownstreamKey, error) {
	return s.queryKeys(
		`SELECT id, key_hash, key_prefix, name, rpm_limit, enabled, created_at, updated_at
		 FROM downstream_keys ORDER BY created_at DESC`,
	)
}

// GetAllKeys returns all downstream keys (equivalent to ListKeys; provided for snapshot loading).
func (s *Store) GetAllKeys() ([]DownstreamKey, error) {
	return s.ListKeys()
}

// UpdateKey updates mutable fields of a downstream key.
func (s *Store) UpdateKey(id int64, name string, rpmLimit int, enabled bool) (*DownstreamKey, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		`UPDATE downstream_keys SET name=?, rpm_limit=?, enabled=?, updated_at=? WHERE id=?`,
		name, rpmLimit, enabled, now, id,
	)
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
		`SELECT id, key_hash, key_prefix, name, rpm_limit, enabled, created_at, updated_at
		 FROM downstream_keys WHERE id=?`, id,
	)
	var dk DownstreamKey
	if err := row.Scan(&dk.ID, &dk.KeyHash, &dk.KeyPrefix, &dk.Name, &dk.RPMLimit, &dk.Enabled, &dk.CreatedAt, &dk.UpdatedAt); err != nil {
		return nil, fmt.Errorf("scan updated key: %w", err)
	}
	return &dk, nil
}

// DeleteKey removes a downstream key by ID.
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

// queryKeys is a helper that runs a SELECT query and scans DownstreamKey rows.
func (s *Store) queryKeys(query string, args ...interface{}) ([]DownstreamKey, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query downstream keys: %w", err)
	}
	defer rows.Close()

	var result []DownstreamKey
	for rows.Next() {
		var dk DownstreamKey
		if err := rows.Scan(&dk.ID, &dk.KeyHash, &dk.KeyPrefix, &dk.Name, &dk.RPMLimit, &dk.Enabled, &dk.CreatedAt, &dk.UpdatedAt); err != nil {
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
// Request Log
// ---------------------------------------------------------------------------

// InsertRequestLogBatch inserts a batch of request logs in a single transaction.
func (s *Store) InsertRequestLogBatch(logs []RequestLog) error {
	if len(logs) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	stmt, err := tx.Prepare(
		`INSERT INTO request_logs (downstream_key_id, upstream_name, client_ip, provider_style, path, status_code, latency_ms, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
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
		if _, err = stmt.Exec(log.DownstreamKeyID, log.UpstreamName, log.ClientIP, log.ProviderStyle, log.Path, log.StatusCode, log.LatencyMs, createdAt); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert request log: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit batch insert: %w", err)
	}
	return nil
}

// DeleteLogsOlderThan removes request logs older than the given duration from now.
func (s *Store) DeleteLogsOlderThan(d time.Duration) error {
	cutoff := time.Now().UTC().Add(-d)
	if _, err := s.db.Exec(`DELETE FROM request_logs WHERE created_at < ?`, cutoff); err != nil {
		return fmt.Errorf("delete old logs: %w", err)
	}
	return nil
}

// QueryLogs retrieves request logs for a given key within a time range.
// Pass keyID=0 to query across all keys. limit<=0 means no limit.
func (s *Store) QueryLogs(keyID int64, from, to time.Time, limit int) ([]RequestLog, error) {
	query := `SELECT id, downstream_key_id, upstream_name, client_ip, provider_style, path, status_code, latency_ms, created_at
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
		if err := rows.Scan(&rl.ID, &rl.DownstreamKeyID, &rl.UpstreamName, &rl.ClientIP, &rl.ProviderStyle, &rl.Path, &rl.StatusCode, &rl.LatencyMs, &rl.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan request log row: %w", err)
		}
		result = append(result, rl)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate request log rows: %w", err)
	}
	return result, nil
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

// ---- Model Whitelist ----

// ListModelWhitelist returns all whitelist patterns.
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

// AddModelWhitelist inserts a new pattern into the whitelist.
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

// DeleteModelWhitelist removes a pattern by ID.
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
// Key ↔ Upstream Bindings
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
