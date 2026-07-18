package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

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
