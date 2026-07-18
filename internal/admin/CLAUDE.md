[根目录](../../CLAUDE.md) > [internal](../) > **admin**

# internal/admin — 管理面板与管理 API

> 最后更新：2026-05-15 15:03:30

## 模块职责

提供运维控制台：

1. 单页管理面板（`static/index.html` + `admin.css` + `admin.js`，`go:embed` 进二进制；仍零 npm 依赖）
2. JSON REST API：上游 / 下游 Key / 绑定 / 模型路由覆盖 / 模型白名单 / 测试模型 / 日志查询 / 系统状态 / 设置
3. 上游连通性 & Key 测试（OpenAI / Anthropic / Responses 三种协议；可携带 CF 绕过参数）
4. 上游额度查询（new-api 风格 `/api/usage/token`）
5. 业务规则的输入校验：SSRF 防护（`validateBaseURL`）、代理 URL 校验、glob 模式预检、唯一/外键约束的 400 化

所有 `/admin/api/*` 端点强制 Bearer Token 认证（`authMiddleware`）。

## 入口与启动

| 文件 | 说明 |
|------|------|
| `handler.go` | 主 handler；构造函数 + 全部路由 + 业务逻辑 |
| `static.go` | `go:embed static/*`；`dashboardHTML` + `/admin/assets/` 文件服务 |
| `static/index.html` | 页面结构（登录壳、tabs、对话框） |
| `static/admin.css` | 样式 |
| `static/js/*.js` | 前端逻辑按域拆分（vanilla，无构建；经典 script 顺序加载） |

构造与注册：

```go
adminHandler := admin.NewAdminHandler(db, keyCache, rateLimiter, prober, dynamicProxy,
    auditLogger, modelFilter, globalCounter, perKeyStats, overrideCache,
    adminToken, version)
adminHandler.RegisterRoutes(r)   // r 是 mux.Router
```

## 对外接口

完整路由表（按 `RegisterRoutes` 注册顺序）：

| Method | Path | Handler | 说明 |
|--------|------|---------|------|
| GET | `/admin/api/upstreams` | `listUpstreams` | 列出上游（含 Key 详情、调度模式、备注、enabled） |
| POST | `/admin/api/upstreams` | `createUpstream` | 兼容 `api_key`(单)/`api_keys`(多) |
| PUT | `/admin/api/upstreams/{id}` | `updateUpstream` | 部分更新；提供 `api_keys` 时全量替换，空数组表示清空 |
| DELETE | `/admin/api/upstreams/{id}` | `deleteUpstream` | CASCADE 删除 Key/模式/绑定/覆盖 |
| POST | `/admin/api/upstreams/{id}/test-proxy` | `testUpstreamProxy` | 用上游代理 GET `/v1/models` 验证连通性 |
| POST | `/admin/api/upstreams/{id}/check-quota` | `checkUpstreamQuota` | 仅识别 new-api 风格 `data.object=token_usage` |
| GET | `/admin/api/upstreams/models` | `getAllUpstreamModelPatterns` | 一次拉取所有上游模式（管理页批量渲染） |
| GET/PUT | `/admin/api/upstreams/{id}/models` | `get/setUpstreamModelPatterns` | 全量覆盖；`patterns` 字段必填（`null` → 400） |
| GET | `/admin/api/upstreams/{id}/apikeys` | `listUpstreamAPIKeys` | 含 `row_id`/`enabled` |
| POST | `/admin/api/upstreams/{id}/apikeys` | `addUpstreamAPIKeys` | 追加 Key，不影响现有 Key |
| DELETE | `/admin/api/upstreams/{id}/apikeys/{key_id}` | `deleteUpstreamAPIKey` | 删除单个 Key |
| PUT | `/admin/api/upstreams/{id}/apikeys/{key_id}/enabled` | `setAPIKeyEnabled` | 单 Key 启停 |
| POST | `/admin/api/upstreams/{id}/apikeys/{key_id}/test` | `testUpstreamAPIKey` | 协议自动构造 + 解析 reply |
| GET | `/admin/api/keys` | `listKeys` | 不含明文，只返回 `key_prefix` |
| POST | `/admin/api/keys` | `createKey` | **明文仅返回一次** |
| PUT | `/admin/api/keys/{id}` | `updateKey` | 部分更新（pointer 字段） |
| DELETE | `/admin/api/keys/{id}` | `deleteKey` | 同步清理 KeyCache / RateLimiter / PerKeyStats / OverrideCache |
| GET | `/admin/api/keys/{id}/reveal` | `revealKey` | 解密返回明文（旧 Key 返回 410 Gone） |
| GET | `/admin/api/logs` | `queryLogs` | `key_id, from, to, limit`（RFC3339） |
| GET | `/admin/api/logs/key-stats` | `getKeyUsageStats` | 按 Key 聚合（total/success/error/avg_latency） |
| GET/POST | `/admin/api/models/whitelist` | `list/addModelWhitelist` | 写入前 `path.Match` 预检 |
| DELETE | `/admin/api/models/whitelist/batch` | `batchDeleteModelWhitelist` | `{"ids":[...]}` |
| DELETE | `/admin/api/models/whitelist/{id}` | `deleteModelWhitelist` | 单条 |
| GET | `/admin/api/keys/bindings` | `getAllKeyBindings` | 总览 |
| GET/PUT | `/admin/api/keys/{id}/upstreams` | `get/setKeyUpstreams` | 全量覆盖；空数组 = 清空显式绑定（回退默认路由） |
| GET | `/admin/api/keys/model-overrides` | `getAllKeyModelOverrides` | 总览 |
| GET/PUT | `/admin/api/keys/{id}/model-overrides` | `get/setKeyModelOverrides` | 全量覆盖；同 (pattern, upstream_id) 去重 |
| GET | `/admin/api/status` | `getStatus` | 健康上游列表 / 总 Key 数 / 当日请求数 / RPM / RPS / 连接池统计 |
| GET | `/admin/api/key-rpm` | `getKeyRPM` | 单独端点避免 status 轮询负担 |
| GET/POST/PUT/DELETE | `/admin/api/test-models` (+ `/{id}`) | `*TestModel` | 测试对话框可复用模型 |
| GET/PUT | `/admin/api/settings` | `get/updateSettings` | 当前仅 `auto_disable_threshold`（同步写 `settings` 表 + atomic store） |
| GET | `/admin/assets/*` | `assetsHandler` | 嵌入的 CSS/JS（`Cache-Control: no-cache`） |
| `*` | `/admin/...` | `serveDashboard` | 兜底返回 `static/index.html` |

## 关键依赖与配置

- `internal/store` — 所有持久化操作（CRUD + 加解密）
- `internal/middleware` — `KeyCache.Reload`、`RateLimiter.RemoveKey`、`ModelFilter.Reload`、`ModelOverrideCache.Reload`、`PerKeyStatsCollector.RemoveKey`、`AuditLogger.DroppedCount`
- `internal/proxy` — `DynamicProxy`（读 `AllUpstreams` / `AutoDisableThreshold`）、`UpstreamProber.ProbeNow`、`BuildTransport` / `RemoveTransport` / `TransportPoolStats`
- `github.com/gorilla/mux` — 子路由 + path 变量
- 标准库 `net.LookupHost` — SSRF 校验解析主机名

## 安全约束

- **SSRF 防御**：`validateBaseURL` 解析 host → 拒绝 loopback/private/link-local/unspecified；scheme 限定 http/https
- **代理 URL 校验**：`validateProxyURL` 限定 scheme（http/https/socks5）+ 必须有 hostname
- **凭据脱敏**：日志只输出 `sanitizeProxyForLog` 后的代理 URL（移除 user info）
- **重定向防御**：所有出站测试 client 都拒跟随重定向（防 302 → 内网 SSRF），`checkUpstreamQuota` 例外但限制同 host 且 ≤5 跳
- **响应体限流**：测试响应读取 `io.LimitReader(..., 256KB)`、quota 限 64KB
- **明文密钥仅返回一次**：`createKey` 直接返回；`revealKey` 解密历史明文（v12+ 创建的）
- **管理 Token**：使用 `subtle.ConstantTimeCompare` 常量时间比较（Bearer 头与 SSE `/events` 查询参数两处）

## 扩展点

| 想做的事 | 修改位置 |
|---------|---------|
| 新 admin API | `RegisterRoutes` 注册 + 同步根 CLAUDE.md 与本文件 |
| 替换 dashboard 前端 | 改 `static/index.html` / `admin.css` / `static/js/*`；`go build` 自动 embed，无需 npm |
| 新增 setting 项 | `getSettings` + `updateSettings` 增加字段 + `store.GetSetting/SetSetting` 持久化 |
| 支持新上游协议测试 | `testUpstreamAPIKey` 的 `switch req.Protocol` 中加 case |
| 新增连接池可观测项 | 修改 `proxy.TransportPoolStats()`，在 `getStatus` 自动暴露 |

## 测试与质量

模块当前无 `*_test.go`（admin 主要是 wire-up）。功能正确性依赖：
- 底层 store / middleware 单测覆盖
- 手工 e2e（dashboard / curl）

如需引入测试：用 `httptest.NewRecorder` + `mux.NewRouter` + 内存 SQLite 模拟。

## 常见问题 (FAQ)

- **PUT 上游后绑定/覆盖为何不消失**：上游被删除时 FK CASCADE 触发；handler 在 `deleteUpstream` 后会调 `overrideCache.Reload()` 同步内存
- **dashboard 加载白屏**：检查 `/admin/assets/` 是否注册在 catch-all `/admin/` 之前；浏览器网络面板看 CSS/JS 是否 404
- **`createUpstream` 返回 400 "private/loopback IP"**：开发环境想用本地 ollama？需要在 `validateBaseURL` 加白名单或用 `127.0.0.1` 之外的可路由地址

## 相关文件清单

- `internal/admin/handler.go`
- `internal/admin/static.go`
- `internal/admin/static/index.html`
- `internal/admin/static/admin.css`
- `internal/admin/static/js/core.js`
- `internal/admin/static/js/upstreams.js`
- `internal/admin/static/js/upstream-test.js`
- `internal/admin/static/js/keys.js`
- `internal/admin/static/js/models.js`
- `internal/admin/static/js/logs.js`
- `internal/admin/static/js/tools.js`
- `internal/admin/static/js/status.js`

## 变更记录 (Changelog)

- 2026-07-11：前端从 `templates.go` 拆为 embed 静态文件；JS 再按域拆到 `static/js/`
- 2026-05-15 15:03:30：初始化模块文档
