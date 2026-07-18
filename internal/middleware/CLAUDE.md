[根目录](../../CLAUDE.md) > [internal](../) > **middleware**

# internal/middleware — HTTP 中间件链

> 最后更新：2026-07-18

## 模块职责

提供构成 `/v1/` 代理链的全部 HTTP 中间件，以及它们依赖的内存运行时数据结构（KeyCache、Limiter、AuditLogger、StatsCollector、ModelOverrideCache、ModelFilter）。

设计原则：

- **无锁热路径优先**：所有"快照"用 `atomic.Value` 整体替换，请求路径无需加锁
- **fail-closed**：绑定查询失败 → 503（避免授权边界静默放宽）
- **职责单一**：每个中间件只做一件事，组合关系由 main.go 决定

## 入口与启动

| 文件 | 关键导出 |
|------|---------|
| `context.go` | `StyleFromContext`, `KeyHashFromContext`, `ResolvedKeyFromContext`, `DownstreamKeyIDFromContext` |
| `cors.go` | `CORSMiddleware` |
| `classifier.go` | `RequestClassifierMiddleware` |
| `keyresolver.go` | `KeyCache`, `NewKeyCache`, `(*KeyCache).Reload`, `KeyResolverMiddleware`, `ResolvedKey` |
| `bindingmw.go` | `ModelOverrideCache`, `NewModelOverrideCache`, `(*ModelOverrideCache).Reload/Get`, `UpstreamBindingMiddleware` |
| `ratelimit_v2.go` | `PerKeyRPMLimiter`, `NewPerKeyRPMLimiter`, `RateLimitMiddleware`, `(*PerKeyRPMLimiter).RemoveKey` |
| `auditlog.go` | 审计中间件入口、请求元数据提取和客户端 IP 解析 |
| `audit_logger.go` | `AuditLogger`、异步队列、批量写入与关闭流程 |
| `audit_response.go` | `responseStatusCapture`、流式接口和内部响应头捕获 |
| `audit_capture.go` | 请求/响应体捕获、大小限制与共享内存预算 |
| `streaming.go` | `StreamingMiddleware` |
| `modelfilter.go` | `ModelFilter`, `NewModelFilter`, `(*ModelFilter).Reload/MatchModel`, `ModelFilterMiddleware` |
| `stats_middleware.go` | `StatsMiddleware`, `PerKeyStatsMiddleware` |
| `request_counter.go` | `GlobalRequestCounter` (RPM/RPS), `PerKeyStatsCollector` |
| `authrewrite.go` | `AuthRewriteMiddleware`（**已退役**：DynamicProxy 内部直接处理，main 未装配） |

中间件统一签名：`func(http.Handler) http.Handler`，由 `cmd/llm-proxy/main.go` 用闭包包装链。

## 对外接口

### 中间件清单 & 拒绝码

| 中间件 | 拒绝条件 | 状态码 | 备注 |
|--------|---------|--------|------|
| CORSMiddleware | OPTIONS 预检 | 200 | 加 `Access-Control-*` 头 |
| StatsMiddleware | — | — | 无副作用，仅 `counter.Increment()` |
| RequestClassifier | 无 Key | 401 `missing API key` | 写入 `ctxKeyStyle` / `ctxKeyHash` |
| KeyResolver | 未知 / disabled | 401 `invalid or disabled API key` | 写入 `ctxKeyResolvedKey` / `ctxKeyDownstreamKeyID` |
| PerKeyStats | — | — | `ResolvedKey` 为 nil 时跳过 |
| UpstreamBinding | 存储错误 | 503 `upstream binding lookup failed` | 写入 allowed IDs + override rules（用 proxy 包私有 ctx key）|
| RateLimit | 超限 | 429 + `Retry-After` 头 | `RPMLimit <= 0` 表示无限 |
| AuditLog | — | — | 包装 `responseStatusCapture` 拦截内部头 |
| Streaming | — | — | 仅在已知流式端点 + Flusher 可用时包装 |
| ModelFilter | — | — | 只拦截 `GET /v1/models`，对响应 JSON 做白名单过滤 |

### KeyCache（原子快照）

```go
kc := middleware.NewKeyCache()
kc.Reload(store)                   // admin 修改 Key 后调用
kc.get().Lookup(hash) *ResolvedKey // 仅供 KeyResolverMiddleware 使用
```

`KeySnapshot.keys` 是 `map[string]*ResolvedKey`（key_hash → 元数据），整体 `atomic.Value.Store` 替换。

### PerKeyRPMLimiter（滑动窗口）

每个 keyID 维护 `slidingWindow{ timestamps []time.Time }`；`countInWindow` 用 in-place compaction 清理过期戳。返回 `(allowed, retryAfterSeconds)`。

### AuditLogger（异步批写）

```go
al := middleware.NewAuditLogger(store, geo, bufferSize=10000, batchSize=100, 1*time.Second)
al.Log(record)         // 入队，通道满 → atomic 计数 dropped 并丢弃
al.DroppedCount() int64
al.Stop()              // close(stopCh) → run() 排空剩余 → close(done)；sync.Once 保证幂等
```

设计点：
- `stopCh` 模式避免 `close(ch)` 与 `send` 的数据竞争
- `run()` 在 stop 时排空通道再 flush
- `geoIP` 在写 goroutine 中补 `IPRegion`（不在请求热路径）

`responseStatusCapture` 在 `WriteHeader` 时读并删除以下内部头（与 proxy 包契约）：
- `X-Upstream-Name`、`X-API-Key-Index`、`X-Model`、`X-Used-Proxy`

客户端 IP 提取优先级：`CF-Connecting-IP` > `X-Real-IP` > `X-Forwarded-For`(取首个) > `RemoteAddr` (去端口)

### ModelOverrideCache（per-Key 模型路由覆盖）

```go
oc := middleware.NewModelOverrideCache(store)  // 启动时加载
oc.Reload()                                    // admin 修改后调用
oc.Get(keyID) []proxy.KeyModelOverrideRule
```

加载时校验 glob pattern；非法 pattern 被跳过 + 告警，不阻塞启动。同 `ModelPattern` 的多行合并为一条 rule（多个 UpstreamID = failover 列表）。

### ModelFilter（全局白名单）

- `MatchModel(modelID)` 空白名单 → 全部允许；含 `*`/`?` 用 `path.Match`，否则精确等值
- 同时被注入到 `dp.WhitelistMatcher`（请求拦截）+ 装配为 `ModelFilterMiddleware`（响应过滤）

### GlobalRequestCounter / PerKeyStatsCollector

环形缓冲区（60 个 bucket，每秒一桶），桶用各自 `sync.Mutex` 保护。

- `RPM()` 求和过去 60 秒
- `RPS()` 求平均过去 5 秒（冷启动用实际 elapsed 避免虚高）
- `PerKeyStatsCollector.AllActiveRPMs()` 顺便清理 5 分钟无活动的 key（防内存泄漏）

## 关键依赖与配置

- `internal/store` — `Store.GetAllKeys`、`GetKeyUpstreamIDs`、`GetAllKeyModelOverrides`、`InsertRequestLogBatch`、`ListModelWhitelist`
- `internal/proxy` — Context helpers、`KeyModelOverrideRule`、`ProviderStyle`
- `internal/geoip` — `(*GeoIP).Lookup`（可为 nil）

## 上下文 Key 约定

`context.go` 用 **包内私有 `contextKey string` 类型** + 命名常量；`bindingmw` 用 **proxy 包的私有 struct key**（跨包共享但仍隔离）。**禁止用裸 string 作为 ctx key**（防冲突 + 防外部伪造）。

## 扩展点

| 想做的事 | 修改位置 |
|---------|---------|
| 新中间件 | 加 `xxx.go` + 在 `cmd/llm-proxy/main.go` 装配（参考"中间件链顺序"） |
| 替换限流算法 | `ratelimit_v2.go` 内重写 `Check`；接口保持 `(allowed, retryAfter)` |
| 异步日志改 Kafka | 替换 `audit_logger.go` 中 `auditLogger.run()` 的批量写入调用 |
| 新增内部响应头 | 在 `audit_response.go` 的 `responseStatusCapture.WriteHeader` 同步处理；否则会泄漏到客户端 |
| Bucket 更细粒度 | `request_counter.go` 把 60 改成 600 + 调整索引算法 |

## 测试与质量

| 测试文件 | 主要场景 |
|---------|---------|
| `cors_test.go` | OPTIONS 预检 + 头注入 |
| `streaming_test.go` | 已知端点包装 + flush 行为 |
| `auditlog_test.go` | 异步批写、丢弃计数、Stop 排空、响应头捕获 |
| `bindingmw_test.go` | 绑定查询失败 → 503；空绑定不限制 |

## 常见问题 (FAQ)

- **修改 Key 后立即请求仍 401**：必须在 admin handler 中调 `keyCache.Reload(store)`；admin 的 `createKey/updateKey/deleteKey` 已自动处理
- **限流 429 但 RPM 没满**：滑动窗口与日历分钟不同；以请求时刻向前 60s 为窗口
- **审计日志落库慢/丢弃**：调大 `audit.batch_size` / `channel_buffer` / 减小 `flush_interval_ms`；`DroppedCount` 暴露在 `/admin/api/status`
- **CORS 不生效**：CORSMiddleware 仅装配在 `/v1/`；admin API 故意不开 CORS

## 相关文件清单

- `internal/middleware/*.go`（13 个源文件 + 4 个测试文件）

## 变更记录 (Changelog)

- 2026-07-18：将审计中间件按采集、异步写入、响应捕获和入口职责拆分
- 2026-05-15 15:03:30：初始化模块文档
