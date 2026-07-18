package store

import (
	"fmt"
	"time"
)

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
