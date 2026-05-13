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
	{
		// v8: 为每个上游配置支持的模型模式（glob），实现按 model 字段路由。
		// 没有配置任何模式的上游视为"支持所有模型"（向后兼容）。
		// 外键级联删除保证删除上游时自动清理关联模式。
		version: 8,
		up: `
CREATE TABLE IF NOT EXISTS upstream_model_patterns (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    upstream_id INTEGER NOT NULL REFERENCES upstream_providers(id) ON DELETE CASCADE,
    pattern     TEXT NOT NULL,
    created_at  DATETIME,
    UNIQUE(upstream_id, pattern)
);
CREATE INDEX IF NOT EXISTS idx_upstream_model_patterns_upstream ON upstream_model_patterns (upstream_id);
`,
	},
	{
		// v9: 为请求日志增加 IP 归属地字段，由 ip2region 在日志批写时填充。
		version: 9,
		up: `
ALTER TABLE request_logs ADD COLUMN ip_region TEXT NOT NULL DEFAULT '';
`,
	},
	{
		// v10: 为每个下游 Key 配置 per-model 的上游路由覆盖。
		// 一个 key + 一个 model pattern 可以映射到多个上游（多行），支持 failover。
		// 外键级联删除保证删除 key 或 upstream 时自动清理关联覆盖规则。
		version: 10,
		up: `
CREATE TABLE IF NOT EXISTS key_model_overrides (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    downstream_key_id INTEGER NOT NULL REFERENCES downstream_keys(id) ON DELETE CASCADE,
    model_pattern     TEXT NOT NULL,
    upstream_id       INTEGER NOT NULL REFERENCES upstream_providers(id) ON DELETE CASCADE,
    created_at        DATETIME,
    UNIQUE(downstream_key_id, model_pattern, upstream_id)
);
CREATE INDEX IF NOT EXISTS idx_key_model_overrides_key ON key_model_overrides (downstream_key_id);
`,
	},
	{
		// v11: 支持每个上游配置多个 API Key，实现轮询调度。
		// 新建 upstream_api_keys 表存储多 Key（加密存储），
		// 并把旧 upstream_providers.api_key 列现有数据迁移到新表。
		// 旧列保留（NOT NULL 约束不方便删除），但运行时不再使用。
		version: 11,
		up: `
CREATE TABLE IF NOT EXISTS upstream_api_keys (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    upstream_id INTEGER NOT NULL REFERENCES upstream_providers(id) ON DELETE CASCADE,
    api_key     TEXT NOT NULL,
    created_at  DATETIME
);
CREATE INDEX IF NOT EXISTS idx_upstream_api_keys_upstream ON upstream_api_keys (upstream_id);

-- 把旧的单 Key 迁移到新表（仅非空的），保留加密格式不变
INSERT INTO upstream_api_keys (upstream_id, api_key, created_at)
SELECT id, api_key, updated_at FROM upstream_providers WHERE api_key != '';
`,
	},
	{
		// v12: 为下游密钥增加加密存储的明文字段，支持密钥二次复制。
		// 旧密钥的 key_encrypted 为空，仅新创建的密钥可复制。
		version: 12,
		up: `
ALTER TABLE downstream_keys ADD COLUMN key_encrypted TEXT NOT NULL DEFAULT '';
`,
	},
	{
		// v13: 支持单个 API Key 启用/禁用 + Key 调度模式（round-robin / fill）。
		// upstream_api_keys.enabled: 允许单独禁用某个 Key 而不影响其他 Key。
		// upstream_providers.key_scheduling_mode: 控制多 Key 调度策略。
		//   - round-robin（默认）: 依次轮询每个 Key。
		//   - fill: 优先使用当前 Key 直到出错，再切换到下一个。
		version: 13,
		up: `
ALTER TABLE upstream_api_keys ADD COLUMN enabled BOOLEAN NOT NULL DEFAULT 1;
ALTER TABLE upstream_providers ADD COLUMN key_scheduling_mode TEXT NOT NULL DEFAULT 'round-robin';
`,
	},
	{
		// v14: 测试模型管理 —— 存储常用的测试模型名称，供测试对话框快速选择。
		// 不同协议（openai/anthropic/responses）可以有同名模型，按协议区分。
		version: 14,
		up: `
CREATE TABLE IF NOT EXISTS test_models (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    protocol   TEXT NOT NULL DEFAULT 'openai',
    created_at DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_test_models_name_protocol ON test_models (name, protocol);
`,
	},
	{
		// v15: 请求日志增加上游 API Key 索引字段，记录每次请求使用了哪个 Key。
		version: 15,
		up: `
ALTER TABLE request_logs ADD COLUMN upstream_key_idx INTEGER NOT NULL DEFAULT -1;
`,
	},
	{
		// v16: 请求日志增加模型名称字段，记录每次请求的 model。
		version: 16,
		up: `
ALTER TABLE request_logs ADD COLUMN model TEXT NOT NULL DEFAULT '';
`,
	},
	{
		// v17: 上游增加备注字段 + 失效 Key 自动禁用相关字段。
		// remark: 管理员备注（如 Key 来源、用途）。
		// consecutive_failures: 连续失败次数，用于自动禁用判定。
		version: 17,
		up: `
ALTER TABLE upstream_providers ADD COLUMN remark TEXT NOT NULL DEFAULT '';
ALTER TABLE upstream_api_keys ADD COLUMN consecutive_failures INTEGER NOT NULL DEFAULT 0;
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
