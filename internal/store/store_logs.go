package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// 请求日志
// ---------------------------------------------------------------------------

// InsertRequestLogBatch 在单个事务中批量插入请求日志。
func (s *Store) InsertRequestLogBatch(logs []RequestLog) error {
	if len(logs) == 0 {
		return nil
	}
	resolvedSessions, err := s.resolveRequestLogBatchSessions(logs)
	if err != nil {
		return fmt.Errorf("resolve request log sessions: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	logStmt, err := tx.Prepare(
		`INSERT INTO request_logs (downstream_key_id, upstream_name, upstream_key_idx, model, used_proxy, client_ip, ip_region, provider_style, path, status_code, latency_ms, request_size, response_size, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare insert statement: %w", err)
	}
	defer logStmt.Close()

	detailStmt, err := tx.Prepare(
		`INSERT INTO request_log_details (request_log_id, session_id, session_source, session_preview, response_id, parent_response_id, method, raw_query, request_headers_json, request_body, request_body_truncated, response_headers_json, response_body, response_body_truncated, capture_status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare request detail insert: %w", err)
	}
	defer detailStmt.Close()
	for i, log := range logs {
		createdAt := log.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		result, execErr := logStmt.Exec(log.DownstreamKeyID, log.UpstreamName, log.UpstreamKeyIdx, log.Model, log.UsedProxy, log.ClientIP, log.IPRegion, log.ProviderStyle, log.Path, log.StatusCode, log.LatencyMs, log.RequestSize, log.ResponseSize, createdAt)
		if execErr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert request log: %w", execErr)
		}
		if log.Detail == nil {
			continue
		}
		logID, idErr := result.LastInsertId()
		if idErr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("read inserted request log id: %w", idErr)
		}
		detail := log.Detail
		sessionID := resolvedSessions[i].id
		sessionSource := resolvedSessions[i].source
		if _, execErr = detailStmt.Exec(logID, sessionID, sessionSource, detail.SessionPreview, detail.ResponseID, detail.ParentResponseID, detail.Method, detail.RawQuery, detail.RequestHeadersJSON, detail.RequestBody, detail.RequestBodyTruncated, detail.ResponseHeadersJSON, detail.ResponseBody, detail.ResponseBodyTruncated, detail.CaptureStatus); execErr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert request log detail: %w", execErr)
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
	return s.QueryLogsFiltered(LogQuery{KeyID: keyID, From: from, To: to, Limit: limit})
}

// QueryLogsFiltered 按组合条件查询轻量日志，并返回是否存在完整记录。
func (s *Store) QueryLogsFiltered(filter LogQuery) ([]RequestLog, error) {
	query := `SELECT l.id, l.downstream_key_id, l.upstream_name, l.upstream_key_idx, l.model, l.used_proxy, l.client_ip, l.ip_region, l.provider_style, l.path, l.status_code, l.latency_ms, l.request_size, l.response_size, l.created_at,
	                 EXISTS(SELECT 1 FROM request_log_details d WHERE d.request_log_id = l.id)
	          FROM request_logs l WHERE l.created_at >= ? AND l.created_at <= ?`
	args := []interface{}{filter.From.UTC(), filter.To.UTC()}

	if filter.KeyID != 0 {
		query += ` AND l.downstream_key_id = ?`
		args = append(args, filter.KeyID)
	}
	if filter.SessionID != "" {
		query += ` AND EXISTS(SELECT 1 FROM request_log_details d WHERE d.request_log_id = l.id AND d.session_id = ?)`
		args = append(args, filter.SessionID)
	}
	if len(filter.SessionKeys) > 0 {
		query += ` AND (`
		for i, session := range filter.SessionKeys {
			if i > 0 {
				query += ` OR `
			}
			query += `(l.downstream_key_id = ? AND EXISTS(SELECT 1 FROM request_log_details d WHERE d.request_log_id = l.id AND d.session_id = ?))`
			args = append(args, session.DownstreamKeyID, session.SessionID)
		}
		query += `)`
	}
	if filter.FullOnly {
		query += ` AND EXISTS(SELECT 1 FROM request_log_details d WHERE d.request_log_id = l.id)`
	}
	if filter.StatusCode != 0 {
		query += ` AND l.status_code = ?`
		args = append(args, filter.StatusCode)
	}
	if filter.Model != "" {
		query += ` AND l.model LIKE ?`
		args = append(args, "%"+filter.Model+"%")
	}
	if filter.Path != "" {
		query += ` AND l.path LIKE ?`
		args = append(args, "%"+filter.Path+"%")
	}

	query += ` ORDER BY l.created_at DESC`

	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query request logs: %w", err)
	}
	defer rows.Close()

	var result []RequestLog
	for rows.Next() {
		var rl RequestLog
		if err := rows.Scan(&rl.ID, &rl.DownstreamKeyID, &rl.UpstreamName, &rl.UpstreamKeyIdx, &rl.Model, &rl.UsedProxy, &rl.ClientIP, &rl.IPRegion, &rl.ProviderStyle, &rl.Path, &rl.StatusCode, &rl.LatencyMs, &rl.RequestSize, &rl.ResponseSize, &rl.CreatedAt, &rl.HasFullRecord); err != nil {
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
		`SELECT l.id, l.downstream_key_id, l.upstream_name, l.upstream_key_idx, l.model, l.used_proxy, l.client_ip, l.ip_region, l.provider_style, l.path, l.status_code, l.latency_ms, l.request_size, l.response_size, l.created_at,
		        EXISTS(SELECT 1 FROM request_log_details d WHERE d.request_log_id = l.id)
		 FROM request_logs l WHERE l.id = ?`, id,
	)
	var rl RequestLog
	if err := row.Scan(&rl.ID, &rl.DownstreamKeyID, &rl.UpstreamName, &rl.UpstreamKeyIdx, &rl.Model, &rl.UsedProxy, &rl.ClientIP, &rl.IPRegion, &rl.ProviderStyle, &rl.Path, &rl.StatusCode, &rl.LatencyMs, &rl.RequestSize, &rl.ResponseSize, &rl.CreatedAt, &rl.HasFullRecord); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("log %d not found", id)
		}
		return nil, fmt.Errorf("scan log: %w", err)
	}
	return &rl, nil
}

// GetRequestLogDetail 按轻量日志 ID 获取完整记录详情。
func (s *Store) GetRequestLogDetail(logID int64) (*RequestLogDetail, error) {
	row := s.db.QueryRow(
		`SELECT request_log_id, session_id, session_source, session_preview, response_id, parent_response_id, method, raw_query, request_headers_json, request_body, request_body_truncated, response_headers_json, response_body, response_body_truncated, capture_status
		 FROM request_log_details WHERE request_log_id = ?`, logID,
	)
	var detail RequestLogDetail
	if err := row.Scan(&detail.RequestLogID, &detail.SessionID, &detail.SessionSource, &detail.SessionPreview, &detail.ResponseID, &detail.ParentResponseID, &detail.Method, &detail.RawQuery, &detail.RequestHeadersJSON, &detail.RequestBody, &detail.RequestBodyTruncated, &detail.ResponseHeadersJSON, &detail.ResponseBody, &detail.ResponseBodyTruncated, &detail.CaptureStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("request log detail %d not found", logID)
		}
		return nil, fmt.Errorf("scan request log detail: %w", err)
	}
	return &detail, nil
}

// GetRequestLogDetails 批量读取完整记录详情，避免管理端逐条查询长期占用单一 SQLite 连接。
func (s *Store) GetRequestLogDetails(logIDs []int64) (map[int64]*RequestLogDetail, error) {
	result := make(map[int64]*RequestLogDetail, len(logIDs))
	if len(logIDs) == 0 {
		return result, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(logIDs)), ",")
	args := make([]interface{}, len(logIDs))
	for i, id := range logIDs {
		args[i] = id
	}
	rows, err := s.db.Query(
		`SELECT request_log_id, session_id, session_source, session_preview, response_id, parent_response_id, method, raw_query, request_headers_json, request_body, request_body_truncated, response_headers_json, response_body, response_body_truncated, capture_status
		 FROM request_log_details WHERE request_log_id IN (`+placeholders+`)`, args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query request log details: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var detail RequestLogDetail
		if err := rows.Scan(&detail.RequestLogID, &detail.SessionID, &detail.SessionSource, &detail.SessionPreview, &detail.ResponseID, &detail.ParentResponseID, &detail.Method, &detail.RawQuery, &detail.RequestHeadersJSON, &detail.RequestBody, &detail.RequestBodyTruncated, &detail.ResponseHeadersJSON, &detail.ResponseBody, &detail.ResponseBodyTruncated, &detail.CaptureStatus); err != nil {
			return nil, fmt.Errorf("scan request log detail: %w", err)
		}
		copy := detail
		result[detail.RequestLogID] = &copy
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate request log details: %w", err)
	}
	return result, nil
}

type responseSessionKey struct {
	downstreamKeyID int64
	responseID      string
}

type resolvedSession struct {
	id     string
	source string
}

// resolveRequestLogBatchSessions 在写事务前批量读取历史父响应，并解析批内依赖。
// 这样插入循环只做内存查找，也能处理 child 先于 parent 出现在同一批次的情况。
func (s *Store) resolveRequestLogBatchSessions(logs []RequestLog) ([]resolvedSession, error) {
	historical, err := s.queryHistoricalResponseSessions(logs)
	if err != nil {
		return nil, err
	}
	byResponse := make(map[responseSessionKey]int)
	for i, logEntry := range logs {
		if logEntry.Detail == nil || logEntry.Detail.ResponseID == "" {
			continue
		}
		key := responseSessionKey{downstreamKeyID: logEntry.DownstreamKeyID, responseID: logEntry.Detail.ResponseID}
		if _, exists := byResponse[key]; !exists {
			byResponse[key] = i
		}
	}

	resolved := make([]resolvedSession, len(logs))
	states := make([]uint8, len(logs))
	var resolve func(int) resolvedSession
	resolve = func(index int) resolvedSession {
		if states[index] == 2 {
			return resolved[index]
		}
		if states[index] == 1 {
			return resolvedSession{}
		}
		states[index] = 1
		logEntry := logs[index]
		detail := logEntry.Detail
		var session resolvedSession
		switch {
		case detail == nil:
		case detail.SessionID != "":
			session = resolvedSession{id: detail.SessionID, source: detail.SessionSource}
		case detail.ParentResponseID != "":
			parentKey := responseSessionKey{downstreamKeyID: logEntry.DownstreamKeyID, responseID: detail.ParentResponseID}
			if parentIndex, ok := byResponse[parentKey]; ok && parentIndex != index {
				session = resolve(parentIndex)
			}
			if session.id == "" {
				session = historical[parentKey]
			}
			if session.id == "" {
				session = resolvedSession{id: detail.ParentResponseID, source: "previous_response_id"}
			}
		case detail.ResponseID != "":
			session = resolvedSession{id: detail.ResponseID, source: "response_id"}
		}
		resolved[index] = session
		states[index] = 2
		return session
	}
	for i := range logs {
		resolve(i)
	}
	return resolved, nil
}

func (s *Store) queryHistoricalResponseSessions(logs []RequestLog) (map[responseSessionKey]resolvedSession, error) {
	result := make(map[responseSessionKey]resolvedSession)
	seen := make(map[responseSessionKey]struct{})
	pairs := make([]responseSessionKey, 0, len(logs))
	for _, logEntry := range logs {
		if logEntry.Detail == nil || logEntry.Detail.ParentResponseID == "" {
			continue
		}
		key := responseSessionKey{downstreamKeyID: logEntry.DownstreamKeyID, responseID: logEntry.Detail.ParentResponseID}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		pairs = append(pairs, key)
	}
	const lookupBatchSize = 250
	for start := 0; start < len(pairs); start += lookupBatchSize {
		end := start + lookupBatchSize
		if end > len(pairs) {
			end = len(pairs)
		}
		values := strings.TrimSuffix(strings.Repeat("(?, ?),", end-start), ",")
		args := make([]interface{}, 0, 2*(end-start))
		for _, pair := range pairs[start:end] {
			args = append(args, pair.downstreamKeyID, pair.responseID)
		}
		rows, err := s.db.Query(
			`WITH requested(downstream_key_id, response_id) AS (VALUES `+values+`),
			 ranked AS (
			   SELECT requested.downstream_key_id, requested.response_id, d.session_id, d.session_source,
			          ROW_NUMBER() OVER (PARTITION BY requested.downstream_key_id, requested.response_id ORDER BY d.request_log_id DESC) AS row_num
			   FROM requested
			   JOIN request_logs l ON l.downstream_key_id = requested.downstream_key_id
			   JOIN request_log_details d ON d.request_log_id = l.id AND d.response_id = requested.response_id
			   WHERE d.session_id <> ''
			 )
			 SELECT downstream_key_id, response_id, session_id, session_source FROM ranked WHERE row_num = 1`, args...,
		)
		if err != nil {
			return nil, fmt.Errorf("query historical response sessions: %w", err)
		}
		for rows.Next() {
			var key responseSessionKey
			var session resolvedSession
			if err := rows.Scan(&key.downstreamKeyID, &key.responseID, &session.id, &session.source); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan historical response session: %w", err)
			}
			result[key] = session
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate historical response sessions: %w", err)
		}
		rows.Close()
	}
	return result, nil
}

// QueryLogSessions 按最后活动时间倒序返回连续会话汇总。
func (s *Store) QueryLogSessions(filter LogQuery) ([]RequestLogSession, error) {
	query := `WITH filtered AS (
	            SELECT d.session_id, d.session_source, d.session_preview, l.downstream_key_id,
	                   l.created_at, l.id AS request_log_id, l.status_code
	            FROM request_logs l
	            JOIN request_log_details d ON d.request_log_id = l.id
	            WHERE d.session_id <> '' AND l.created_at >= ? AND l.created_at <= ?`
	args := []interface{}{filter.From.UTC(), filter.To.UTC()}
	if filter.KeyID != 0 {
		query += ` AND l.downstream_key_id = ?`
		args = append(args, filter.KeyID)
	}
	if filter.StatusCode != 0 {
		query += ` AND l.status_code = ?`
		args = append(args, filter.StatusCode)
	}
	if filter.Model != "" {
		query += ` AND l.model LIKE ?`
		args = append(args, "%"+filter.Model+"%")
	}
	if filter.Path != "" {
		query += ` AND l.path LIKE ?`
		args = append(args, "%"+filter.Path+"%")
	}
	query += `), ranked AS (
	             SELECT *,
	                    ROW_NUMBER() OVER (
	                        PARTITION BY downstream_key_id, session_id
	                        ORDER BY created_at DESC, request_log_id DESC
	                    ) AS row_num,
	                    ROW_NUMBER() OVER (
	                        PARTITION BY downstream_key_id, session_id
	                        ORDER BY CASE WHEN session_preview <> '' THEN 0 ELSE 1 END,
	                                 created_at DESC, request_log_id DESC
	                    ) AS preview_row_num
	          FROM filtered
	          )
	          SELECT session_id,
	                 MAX(CASE WHEN row_num = 1 THEN session_source END),
	                 MAX(CASE WHEN preview_row_num = 1 THEN session_preview END),
	                 downstream_key_id,
	                 MIN(CAST(created_at AS TEXT)), MAX(CAST(created_at AS TEXT)), COUNT(*),
	                 SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END)
	          FROM ranked
	          GROUP BY downstream_key_id, session_id
	          ORDER BY MAX(CAST(created_at AS TEXT)) DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query request log sessions: %w", err)
	}
	defer rows.Close()
	var sessions []RequestLogSession
	for rows.Next() {
		var session RequestLogSession
		var firstValue, lastValue string
		if err := rows.Scan(&session.SessionID, &session.SessionSource, &session.SessionPreview, &session.DownstreamKeyID, &firstValue, &lastValue, &session.RequestCount, &session.ErrorCount); err != nil {
			return nil, fmt.Errorf("scan request log session: %w", err)
		}
		session.FirstAt, err = parseSQLiteAggregateTime(firstValue)
		if err != nil {
			return nil, fmt.Errorf("parse request log session first_at: %w", err)
		}
		session.LastAt, err = parseSQLiteAggregateTime(lastValue)
		if err != nil {
			return nil, fmt.Errorf("parse request log session last_at: %w", err)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate request log sessions: %w", err)
	}
	return sessions, nil
}

func parseSQLiteAggregateTime(value string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported SQLite time %q", value)
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
