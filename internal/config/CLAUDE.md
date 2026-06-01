[根目录](../../CLAUDE.md) > [internal](../) > **config**

# internal/config — YAML 配置加载

> 最后更新：2026-05-15 15:03:30

## 模块职责

只做一件事：把 `configs/base.yml` 与 `configs/${ENVIRONMENT}.yml` 合并，填充默认值，输出 `*YAMLConfig` 给 main 使用。设计极简，不做"配置中心"或"运行时热重载"。

运行时可热改的配置项不在这里——见 `store.settings` 表（如 `auto_disable_threshold`）。

## 入口与启动

唯一文件：`config.go`（~185 行）。

```go
yamlConfig, err := config.LoadEnvironmentConfig()  // 失败时由 main 回退到 GetDefaultYAMLConfig()
yamlConfig.LogConfiguration(slog.Default())
```

## 对外接口

```go
type YAMLConfig struct {
    Server   ServerConfig   // Port
    Storage  StorageConfig  // SQLitePath
    Admin    AdminConfig    // Enabled
    Upstream UpstreamConfig // ProbeIntervalSeconds, ProbeTimeoutSeconds, AutoDisableThreshold
    Audit    AuditConfig    // Enabled, BatchSize, FlushInterval(ms), ChannelBuffer
    Logging  LoggingConfig  // Level, Format
}

GetDefaultYAMLConfig() *YAMLConfig
LoadYAMLConfig(filename string) (*YAMLConfig, error)
LoadEnvironmentConfig() (*YAMLConfig, error)   // base.yml + {env}.yml overlay
(*YAMLConfig).Validate() error                  // 填默认值
(*YAMLConfig).LogConfiguration(logger)
```

加载流程：

1. `configDir = "configs"`（写死，相对工作目录）
2. 读 `base.yml`；不存在 → 默认值（`GetDefaultYAMLConfig`）
3. `env := os.Getenv("ENVIRONMENT")`，缺省 `"dev"`
4. 读 `{env}.yml`；存在则 `yaml.Unmarshal(data, baseConfig)`（**直接覆盖到同一对象**，未提供字段保留 base 值）
5. `Validate()` 填默认值（端口 9002、SQLite 路径、探活间隔 30s/超时 5s、自动禁用阈值 2、批写 100、刷新 1000ms、缓冲 10000、日志 info/text）

## 配置文件矩阵

| 文件 | 说明 |
|------|------|
| `configs/base.yml` | 通用默认（端口、SQLite 路径、admin enabled、prober/audit 默认） |
| `configs/dev.yml` | 仅覆盖日志为 debug/text |
| `configs/staging.yml` | （存在但内容简短） |
| `configs/production.yml` | port=80, log=info/json, audit batch=200/flush=2000 |

环境变量覆盖发生在 main 装配时（`PORT` / `LOG_LEVEL` / `LOG_FORMAT`），不在本包内。

## 关键依赖与配置

- `gopkg.in/yaml.v3` — 解析
- 标准库 `log/slog` — 摘要日志输出

## 扩展点

| 想做的事 | 修改位置 |
|---------|---------|
| 新增配置项 | 在对应 `*Config` struct 加字段（带 `yaml:"xxx"` tag）+ `Validate` 填默认 + `GetDefaultYAMLConfig` + `LogConfiguration` 摘要 |
| 新增环境 | 在 `configs/` 加 `xxx.yml` + 启动设置 `ENVIRONMENT=xxx` |
| 切到 envconfig / koanf | 重写 `LoadEnvironmentConfig`，签名保持兼容 |

## 测试与质量

| 测试文件 | 覆盖 |
|---------|------|
| `config_test.go` | 默认值、文件不存在回退、覆盖逻辑、Validate 边界 |

## 常见问题 (FAQ)

- **改了 dev.yml 不生效**：检查 `ENVIRONMENT` 是否真为 `dev`；env 覆盖优先于 yaml
- **想要绝对路径 SQLite**：直接 `sqlite_path: "/var/lib/llm-proxy/db.sqlite"`，main 会按目录前缀 mkdir
- **生产想关闭 admin**：`admin.enabled: false`；同时 `ADMIN_TOKEN` 不再强制（main 检查依赖 enabled）

## 相关文件清单

- `internal/config/config.go`
- `internal/config/config_test.go`
- `configs/base.yml`、`dev.yml`、`staging.yml`、`production.yml`

## 变更记录 (Changelog)

- 2026-05-15 15:03:30：初始化模块文档
