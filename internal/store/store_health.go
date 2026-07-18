package store

import (
	"fmt"
	"time"
)

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
