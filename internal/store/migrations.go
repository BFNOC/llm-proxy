package store

import (
	"database/sql"
	"fmt"
)

// migration represents a single schema migration step.
type migration struct {
	version int
	up      string
}

var migrations = []migration{
	{
		version: 1,
		up: `
CREATE TABLE IF NOT EXISTS _meta (
    schema_version INTEGER NOT NULL DEFAULT 0
);

INSERT INTO _meta (schema_version)
SELECT 0 WHERE NOT EXISTS (SELECT 1 FROM _meta);

CREATE TABLE IF NOT EXISTS upstream_providers (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    base_url   TEXT NOT NULL,
    api_key    TEXT NOT NULL,
    priority   INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS downstream_keys (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    key_hash   TEXT NOT NULL UNIQUE,
    key_prefix TEXT NOT NULL,
    name       TEXT NOT NULL,
    rpm_limit  INTEGER NOT NULL DEFAULT 0 CHECK(rpm_limit >= 0),
    enabled    BOOLEAN NOT NULL DEFAULT 1,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS request_logs (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    downstream_key_id INTEGER NOT NULL,
    provider_style    TEXT NOT NULL,
    path              TEXT NOT NULL,
    status_code       INTEGER NOT NULL,
    latency_ms        INTEGER NOT NULL,
    created_at        DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_request_logs_created_at ON request_logs (created_at);
CREATE INDEX IF NOT EXISTS idx_request_logs_key_id ON request_logs (downstream_key_id);
`,
	},
	{
		version: 2,
		up: `
ALTER TABLE upstream_providers ADD COLUMN enabled BOOLEAN NOT NULL DEFAULT 1;
`,
	},
	{
		version: 3,
		up: `
ALTER TABLE request_logs ADD COLUMN upstream_name TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 4,
		up: `
ALTER TABLE request_logs ADD COLUMN client_ip TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 5,
		up: `
CREATE TABLE IF NOT EXISTS model_whitelist (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern    TEXT NOT NULL UNIQUE,
    created_at DATETIME
);
`,
	},
	{
		// v6: key_upstream_bindings 用持久化方式表达"某个下游 Key 允许访问哪些上游"。
		// 外键和级联删除避免删除 Key 或 Upstream 后留下悬空授权关系；
		// 唯一约束保证重复提交同一绑定时保持幂等。
		version: 6,
		up: `
CREATE TABLE IF NOT EXISTS key_upstream_bindings (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    downstream_key_id INTEGER NOT NULL REFERENCES downstream_keys(id) ON DELETE CASCADE,
    upstream_id       INTEGER NOT NULL REFERENCES upstream_providers(id) ON DELETE CASCADE,
    created_at        DATETIME,
    UNIQUE(downstream_key_id, upstream_id)
);
CREATE INDEX IF NOT EXISTS idx_key_upstream_bindings_key ON key_upstream_bindings (downstream_key_id);
`,
	},
	{
		// v7: 为每个上游增加可选的代理地址，支持 http/https/socks5 协议。
		// 留空表示继承环境代理（HTTP_PROXY 等环境变量）。
		version: 7,
		up: `
ALTER TABLE upstream_providers ADD COLUMN proxy_url TEXT NOT NULL DEFAULT '';
`,
	},
}

// RunMigrations applies all pending schema migrations in order.
func RunMigrations(db *sql.DB) error {
	// Ensure _meta table exists so we can read the current version.
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS _meta (schema_version INTEGER NOT NULL DEFAULT 0)`)
	if err != nil {
		return fmt.Errorf("create _meta table: %w", err)
	}

	// Seed the row if absent.
	_, err = db.Exec(`INSERT INTO _meta (schema_version) SELECT 0 WHERE NOT EXISTS (SELECT 1 FROM _meta)`)
	if err != nil {
		return fmt.Errorf("seed _meta row: %w", err)
	}

	var currentVersion int
	if err = db.QueryRow(`SELECT schema_version FROM _meta`).Scan(&currentVersion); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	for _, m := range migrations {
		if m.version <= currentVersion {
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.version, err)
		}

		if _, err = tx.Exec(m.up); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", m.version, err)
		}

		if _, err = tx.Exec(`UPDATE _meta SET schema_version = ?`, m.version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("update schema version to %d: %w", m.version, err)
		}

		if err = tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.version, err)
		}
	}

	return nil
}
