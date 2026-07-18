[根目录](../../CLAUDE.md) > [internal](../) > **store**

# internal/store — SQLite 持久层

> 最后更新：2026-07-18

## 模块职责

唯一负责持久化的层，封装：

1. **数据库连接**：`modernc.org/sqlite`（纯 Go，无 CGO），`MaxOpenConns=1`（单实例 + WAL，避免锁竞争）
2. **DDL 迁移**：基于 `_meta.schema_version` 的顺序迁移，事务级原子升级
3. **AES-256-GCM 加解密**：上游 API Key、下游 Key 明文（v12+）
4. **CRUD**：上游、下游 Key、API Key 列表、模型模式、Key↔上游绑定、per-Key 模型覆盖、模型白名单、请求日志、测试模型、设置
5. **批量与聚合查询**：避免 N+1（如 `GetAllUpstreamModelPatterns`、`GetAllKeyBindings`）

## 入口与启动

| 文件 | 行数 | 说明 |
|------|------|------|
| `store.go` | 核心 | `Store` 结构、构造、关闭和事务辅助方法 |
| `store_upstreams.go` / `store_upstream_*.go` | 上游 | 上游、上游 Key、速率限制和失败计数 |
| `store_keys.go` / `store_routing.go` | 路由 | 下游 Key、绑定、模型覆盖和白名单 |
| `store_logs.go` / `store_recording.go` | 日志 | 请求日志、批量写入和统计查询 |
| `store_test_models.go` / `store_settings.go` | 配置 | 测试模型和动态设置 |
| `store_health.go` / `store_config.go` | 运维 | 健康检查与配置导入导出 |
| `models.go` | ~93 | `UpstreamProvider`, `APIKeyInfo`, `DownstreamKey`, `RequestLog`, `ModelWhitelistEntry`, `UpstreamModelPattern`, `KeyModelOverride`, `TestModel` |
| `migrations.go` | ~295 | `migrations []migration` + `RunMigrations` |
| `encrypt.go` | ~83 | `Encrypt`/`Decrypt`（v1 前缀格式：`v1:base64(nonce+ciphertext)`） |

构造：

```go
db, err := store.NewStore("./data/llm-proxy.db", encryptionKey32Bytes)
defer db.Close()
```

DSN 固化的 PRAGMA：`journal_mode=WAL`、`busy_timeout=5000`、`foreign_keys=ON`（保证 FK CASCADE 生效）。

## Schema 版本（截至 v19）

| 版本 | 主要变更 |
|------|---------|
| 1 | `_meta` + `upstream_providers` + `downstream_keys` + `request_logs` |
| 2 | `upstream_providers.enabled` |
| 3 | `request_logs.upstream_name` |
| 4 | `request_logs.client_ip` |
| 5 | `model_whitelist` |
| 6 | `key_upstream_bindings`（FK CASCADE + UNIQUE） |
| 7 | `upstream_providers.proxy_url` |
| 8 | `upstream_model_patterns` |
| 9 | `request_logs.ip_region` |
| 10 | `key_model_overrides`（一 key + 一 pattern → 多 upstream） |
| 11 | `upstream_api_keys`（多 Key 支持，旧 `api_key` 列保留兼容） |
| 12 | `downstream_keys.key_encrypted`（明文密钥可二次复制） |
| 13 | `upstream_api_keys.enabled` + `upstream_providers.key_scheduling_mode` |
| 14 | `test_models`（按协议复用） |
| 15 | `request_logs.upstream_key_idx` |
| 16 | `request_logs.model` |
| **17** | **`upstream_providers.remark` + `upstream_api_keys.consecutive_failures`**（fork 扩展） |
| **18** | **`request_logs.used_proxy`**（fork 扩展） |
| 19 | `settings`（key-value 动态配置，如 `auto_disable_threshold`） |

> 新增 schema 变更只能 **追加** 新版本号；改动既有迁移会导致老库无法升级。

## 对外接口（按主题分组）

### Upstream

```go
CreateUpstream(name, baseURL string, apiKeys []string, priority int,
               proxyURL, keySchedulingMode, remark string) (*UpstreamProvider, error)
GetUpstream(id) / ListUpstreams()                         // 自动解密、enabled=1 的 Key
UpdateUpstream(id, ..., apiKeys []string, ...)            // apiKeys=nil 表示不动
DeleteUpstream(id)                                         // CASCADE 清 Key/模式/绑定/覆盖
```

### Upstream API Keys（多 Key + 调度 + 失败追踪）

```go
GetUpstreamAllAPIKeys(upstreamID) []APIKeyInfo            // 含 row_id / enabled / consecutive_failures
GetAllUpstreamAPIKeyRowIDs() map[int64][]int64            // prober 批量加载
SetAPIKeyEnabled(upstreamID, keyRowID, enabled)
IncrKeyFailures(upstreamID, keyRowID, threshold) (count, error)  // 累加 + 达阈值即 enabled=0
ResetKeyFailures(upstreamID, keyRowID)                    // 成功时清零
AutoDisableFailingKeys(threshold) (affected, error)       // prober 周期清理
```

### Downstream Key

```go
CreateKey(name, rpmLimit) (plaintext, *DownstreamKey, error)  // 明文仅返回一次
LookupKeyByHash(hash) / LookupKeyByID(id)
ListKeys() / GetAllKeys()                                  // KeyCache.Reload 用
GetKeyPlaintext(id) string                                 // v12+ 解密；旧 Key 返回 ""
UpdateKey / DeleteKey
```

### 绑定 / 覆盖

```go
SetKeyUpstreams(keyID, upstreamIDs []int64)               // 全量覆盖；空 = 清空（默认路由）
GetKeyUpstreamIDs(keyID) / GetAllKeyBindings()
SetKeyModelOverrides(keyID, []KeyModelOverrideInput)
GetKeyModelOverrides(keyID) / GetAllKeyModelOverrides()
```

### 上游模型模式

```go
SetUpstreamModelPatterns(upstreamID, patterns []string)   // 全量覆盖；空 = 接受所有模型
GetUpstreamModelPatterns / GetAllUpstreamModelPatterns
```

### Whitelist / Logs / Settings / Test Models

```go
ListModelWhitelist / AddModelWhitelist / DeleteModelWhitelist / BatchDeleteModelWhitelist
InsertRequestLogBatch([]RequestLog)                        // audit logger 调用
QueryLogs(keyID, from, to, limit) / CountLogsSince(t) / GetKeyUsageStats
DeleteLogsOlderThan(d)                                     // 当前未自动调度
GetSetting(key, default) / SetSetting(key, value)          // ON CONFLICT UPDATE
ListTestModels(protocol) / Create/Update/DeleteTestModel
CountKeys()
```

## 数据模型 ER 概要

```
upstream_providers (id, name, base_url, priority, enabled, proxy_url,
                    key_scheduling_mode, remark, ...)
   │ 1
   │ ──< upstream_api_keys (id, upstream_id*, api_key[encrypted],
   │                        enabled, consecutive_failures, ...)
   │ ──< upstream_model_patterns (id, upstream_id*, pattern, ...)
   │
downstream_keys (id, key_hash, key_prefix, name, rpm_limit, enabled,
                 key_encrypted, ...)
   │
   │ ──< key_upstream_bindings (id, downstream_key_id*, upstream_id*, ...)
   │ ──< key_model_overrides   (id, downstream_key_id*, model_pattern,
   │                            upstream_id*, ...)
   │
request_logs (id, downstream_key_id, upstream_name, upstream_key_idx,
              model, used_proxy, client_ip, ip_region, provider_style,
              path, status_code, latency_ms, created_at)

model_whitelist (id, pattern UNIQUE, ...)
test_models     (id, name, protocol, ... UNIQUE(name, protocol))
settings        (key PRIMARY KEY, value)
_meta           (schema_version)
```

外键均带 `ON DELETE CASCADE`，删除上游/Key 自动清理派生记录。

## 关键依赖与配置

- `modernc.org/sqlite` — 纯 Go SQLite 驱动（注意：`sql.Open("sqlite", ...)` 而非 `sqlite3`）
- 加密：标准库 `crypto/aes` + `crypto/cipher`（GCM）

## 安全约束

- `encryptionKey` **必须 32 字节**；构造和加解密三处都校验
- 密文格式硬编码 `v1:` 前缀；解密拒绝其他前缀（`unsupported ciphertext version`）
- 所有 SQL 都用占位符 `?`，无字符串拼接 → SQL 注入免疫
- `PRAGMA foreign_keys=ON` 通过 DSN 参数固化（避免每次连接需手动设置）
- `MaxOpenConns=1` 单连接模式；并发由 SQLite 的 WAL + busy_timeout 自动序列化

## 扩展点

| 想做的事 | 修改位置 |
|---------|---------|
| 新增表 / 列 | `migrations.go` 追加新 `migration{version: N+1, up: "..."}`；**不改旧条目** |
| 新增加密版本 | `encrypt.go` 加 `v2:` 前缀分支；解密保留 v1 兼容 |
| 新增聚合查询 | 在对应 `store_*.go` 加方法；尽量提供 `GetAllX()` 批量版本避免 N+1 |
| 切换数据库 | 重写 `NewStore` + 占位符；模型字段不变即可保持上层 API 兼容 |

## 测试与质量

| 测试文件 | 覆盖 |
|---------|------|
| `store_test.go` / `store_*_test.go` | NewStore 校验、加解密、各领域 CRUD、聚合查询、绑定/覆盖、settings、Failure 计数 |

惯用做法：

```go
func newTestStore(t *testing.T) *Store {
    s, _ := NewStore(filepath.Join(t.TempDir(), "test.db"), testKey)
    t.Cleanup(func() { _ = s.Close() })
    return s
}
```

## 常见问题 (FAQ)

- **`encryption key must be 32 bytes`**：检查 `ENCRYPTION_KEY` 环境变量长度
- **迁移卡在 v17**：典型是手工改过表结构导致 `ALTER TABLE ADD COLUMN` 失败；用空库重新跑迁移或手动 SQL 修复
- **删除上游后还能查到日志**：`request_logs` 不带 FK，按设计保留历史；可手动 `DELETE FROM request_logs WHERE upstream_name = '...'`
- **`InsertRequestLogBatch` 慢**：单连接 + WAL 已是 SQLite 最佳；调大 batch_size 减少 commit 次数

## 相关文件清单

- `internal/store/store.go`、`internal/store/store_*.go`
- `internal/store/models.go`
- `internal/store/migrations.go`
- `internal/store/encrypt.go`
- `internal/store/store_test.go`、`internal/store/store_*_test.go`

## 变更记录 (Changelog)

- 2026-07-18：将集中在 `store.go` 的持久化方法按领域拆分，并同步拆分大型测试文件
- 2026-05-15 15:03:30：初始化模块文档（schema v19，含 fork 扩展 v17/v18）
