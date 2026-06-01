[根目录](../../CLAUDE.md) > [cmd](../) > **llm-proxy**

# cmd/llm-proxy — 程序入口与装配

> 最后更新：2026-05-15 15:03:30

## 模块职责

唯一可执行 binary 的入口包。负责：

1. 初始化结构化日志（自定义 PrettyHandler / JSON Handler，时间统一 CST）
2. 加载 YAML 配置（`base.yml` + `${ENVIRONMENT}.yml` 覆盖）+ 环境变量覆盖
3. 校验 `ENCRYPTION_KEY` / `ADMIN_TOKEN`，打开 SQLite Store
4. 构建运行时单例：KeyCache、PerKeyRPMLimiter、DynamicProxy、ModelOverrideCache、UpstreamProber、AuditLogger、ModelFilter、GlobalRequestCounter、PerKeyStatsCollector
5. **按特定顺序**装配 `/v1/` 中间件链（顺序约束见下文）
6. 注册健康/就绪/根路由 + 装配 admin 路由
7. 启动 HTTP server + 后台 prober goroutine，监听 SIGINT/SIGTERM 优雅停机

## 入口与启动

| 文件 | 说明 |
|------|------|
| `main.go` | 唯一文件。`main()` ≈ 270 行，自顶向下顺序执行装配 |

启动等价命令：

```bash
ENCRYPTION_KEY=01234567890123456789012345678901 ADMIN_TOKEN=tok make dev
# 或
go run ./cmd/llm-proxy
```

构建命令使用 `-ldflags="-X main.Version=$GIT_COMMIT -X main.BuildTime=..."` 注入版本信息（见 `Makefile`）。当前硬编码常量 `version = "2.6.0"`。

## 中间件链顺序（CRITICAL，勿调换）

`main.go` 装配代码自下而上书写（`http.Handler` 包装是栈式），实际执行顺序如下：

```
CORSMiddleware                  ← 最外层，处理 OPTIONS / CORS 头
  └─ StatsMiddleware            ← 全局 RPM/RPS 计数（包括 401/429）
     └─ RequestClassifier       ← 探测 OpenAI/Anthropic 风格 + 提取明文 Key
        └─ KeyResolver          ← 在 KeyCache 查 SHA-256，未知/禁用 → 401
           └─ PerKeyStats       ← 已鉴权 per-Key RPM
              └─ UpstreamBinding ← 加载 Key 绑定上游集合 + per-Key 模型覆盖
                 └─ RateLimiter ← 滑动窗口 RPM 限流，超限 → 429
                    └─ AuditLog ← 异步排队请求日志（可选）
                       └─ Streaming ← SSE flush 包装
                          └─ ModelFilter ← /v1/models 响应白名单过滤
                             └─ DynamicProxy ← 代理转发 + 故障切换
```

**为何顺序重要**：

- `RequestClassifier` 必须先解析 Key，否则 `KeyResolver` 无法查找
- `UpstreamBinding` 必须在 `KeyResolver` 之后（依赖 keyID），且必须在 `DynamicProxy` 之前（fail-closed 授权过滤要在 RoundTrip 前发生）
- `AuditLog` 的内部头（`X-Upstream-Name` / `X-API-Key-Index` / `X-Model` / `X-Used-Proxy`）由 `DynamicProxy.forwardResponse` 写入，被 `responseStatusCapture` 在 `WriteHeader` 时拦截删除——若放错位置会泄漏到客户端
- `Stats` 在最外层确保所有到达代理的请求（含 401/429）都被计入 RPM/RPS

## 对外接口（HTTP 路由）

| Method | Path | 说明 |
|--------|------|------|
| GET/HEAD | `/healthz` | 总是返回 `{"status":"ok"}` |
| GET/HEAD | `/readyz` | `dynamicProxy.GetActiveUpstream() == nil` 时 503 |
| GET | `/` | 返回 API 信息 JSON（替代 404） |
| `*` | `/admin/api/...` | admin 子路由（详见 [admin 模块](../../internal/admin/CLAUDE.md)） |
| `*` | `/admin/...` | 单页管理面板 HTML |
| `*` | `/v1/...` | 中间件链 → 反向代理 |

## 关键依赖与配置

- `internal/admin`、`internal/middleware`、`internal/proxy`、`internal/store`、`internal/config`、`internal/geoip`
- `github.com/gorilla/mux` — 路由
- 标准库 `log/slog` — 结构化日志，自定义 handler 输出 `LEVEL [time] msg; k=v` 格式

启动副作用：
- `os.MkdirAll(yamlConfig.Storage.SQLitePath dir, 0o755)` 创建数据目录
- 注入 `dynamicProxy.WhitelistMatcher = modelFilter.MatchModel`（避免 proxy→middleware 循环依赖）
- 注入 `dynamicProxy.KeyFailCallback / KeySuccessCallback`（实现连续失败计数与自动禁用）
- 从 `settings` 表读取 `auto_disable_threshold` 覆盖 YAML 默认值，存入 `dynamicProxy.AutoDisableThreshold`（atomic 支持运行时改）

## 扩展点

| 想做的事 | 修改位置 |
|---------|---------|
| 新增中间件 | 在 `proxyChain = ...(proxyChain)` 链中按依赖顺序插入；同步更新本文件"中间件链顺序"段 |
| 新增非 admin、非 /v1 的路由 | 在 mux router 上直接注册（注意不会经过 CORS / 鉴权） |
| 改写日志格式 | `CustomPrettyHandler.Handle` 或切到 `LOG_FORMAT=json` |
| 改默认端口 | `defaultPort` 常量 + `configs/base.yml` |
| 自定义 GeoIP 路径 | `GEOIP_DB_PATH` 环境变量 |

## 测试与质量

`main.go` 本身无单测（典型 Go 入口包）。装配逻辑通过 e2e/集成测试在 admin/proxy/middleware 层覆盖。

## 常见问题 (FAQ)

- **启动报 `ENCRYPTION_KEY must be exactly 32 bytes`**：原文必须 32 字节，或十六进制必须 64 字符；任何其他长度都会拒绝
- **管理面板 401**：`Authorization: Bearer ${ADMIN_TOKEN}`；ADMIN_TOKEN 与启动时环境变量必须一致
- **`/readyz` 长期 503**：检查 prober 日志（"upstream unhealthy"）；`probe_interval_seconds` 默认 30s，新增上游可能需要等一轮探活

## 相关文件清单

- `cmd/llm-proxy/main.go`

## 变更记录 (Changelog)

- 2026-05-15 15:03:30：初始化模块文档
