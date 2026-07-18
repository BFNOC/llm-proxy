package store

import (
	"database/sql"
	"fmt"

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
