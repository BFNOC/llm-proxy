[根目录](../../CLAUDE.md) > [internal](../) > **proxy**

# internal/proxy — 反向代理核心

> 最后更新：2026-05-15 15:03:30

## 模块职责

实现把请求从下游 `/v1/...` 转发到动态选择的上游服务商的全部纯逻辑：

1. **风格识别**：根据路径/头判定 OpenAI 或 Anthropic 风格
2. **下游 Key 提取**：从 `Authorization: Bearer` 或 `x-api-key` 取出，SHA-256 哈希
3. **认证头重写**：把下游 Key 替换为上游 Key（按风格选择头）
4. **多 Key 调度**：单个上游的多 API Key 支持 `round-robin` / `fill` 两种策略
5. **多上游故障切换**：按优先级顺序尝试，遇 429/401/403 / 网络错误自动 try next
6. **白名单 / 模型路由 / per-Key 覆盖**：在转发前裁剪可用上游集合
7. **错误体脱敏**：上游 4xx/5xx 响应通过 `SanitizeErrorBody` 抹掉敏感信息后再回写
8. **后台探活**：`UpstreamProber` 周期性 HEAD `/v1/models`，原子替换 `*ActiveUpstream` 列表
9. **Transport 缓存**：按 `proxyURL` 缓存 `*http.Transport`，相同代理共享连接池

## 入口与启动

| 文件 | 行数 | 关键导出 |
|------|------|----------|
| `dynamic.go` | ~640 | `DynamicProxy`, `ActiveUpstream`, `KeyModelOverrideRule`, `ContextWith*`, `*FromContext`, `extractModelFromBody` (内部) |
| `prober.go` | ~196 | `UpstreamProber`, `NewUpstreamProber`, `Start`, `ProbeNow` |
| `transport.go` | ~102 | `BuildTransport`, `RemoveTransport`, `TransportPoolStats` |
| `style.go` | ~33 | `ProviderStyle`, `StyleOpenAI`/`StyleAnthropic`, `DetectProviderStyle` |
| `extract.go` | ~47 | `ExtractDownstreamKey`, `HashKey` |
| `authrewrite.go` | ~30 | `RewriteAuthHeaders` |
| `sanitize.go` | ~49 | `SanitizeErrorBody`, `sanitizeRules`, `maxErrorBodySize` |

`DynamicProxy` 实现 `http.Handler`，由 `cmd/llm-proxy/main.go` 包装中间件链后挂到 `/v1/`。

## 对外接口

### `DynamicProxy`

```go
dp := proxy.NewDynamicProxy()
dp.SetAllUpstreams([]*ActiveUpstream{...})  // 由 prober 调用
dp.GetActiveUpstream() *ActiveUpstream      // 第一个（最高优先级），nil 表示无可用
dp.ActiveRequests() int64                   // atomic 当前并发数
dp.AutoDisableThreshold atomic.Int64        // 公开字段，admin 可热改
dp.WhitelistMatcher func(string) bool       // 由 main 注入（避免循环依赖）
dp.KeyFailCallback / KeySuccessCallback     // 失败计数 + 自动禁用回调
dp.ServeHTTP(w, r)                          // 入口：缓冲 body → 选上游 → 转发
```

`ServeHTTP` 决策流（节选）：

```
upstreams = filterUpstreams(allUpstreams, AllowedUpstreamIDs)        # 绑定 → 403 fail-closed
body, _ = io.ReadAll(LimitReader(r.Body, 32MB))                       # 32MB 上限 → 413
model = extractModelFromBody(body)                                    # JSON 顶层 model 字段
if WhitelistMatcher && !match(model) → 403                            # 白名单拒绝
upstreams = filterUpstreamsByModel(upstreams, model)                  # 模型模式过滤
upstreams = filterUpstreams(upstreams, matchModelOverrides(...))     # per-Key 覆盖（可能 422）

for active in upstreams:
    apiKey, idx, rowID = active.NextAPIKey()                          # round-robin / fill
    RewriteAuthHeaders(req, style, apiKey)
    strip untrustedRequestHeaders + Accept-Encoding
    transport = BuildTransport(active.ProxyURL)                       # 按代理缓存
    resp = transport.RoundTrip(req)
    if resp.Status in {429, 401, 403} && !isLast:
        active.MarkKeyFailed(); KeyFailCallback(...); continue
    forwardResponse(w, resp, ...)                                     # 含错误体脱敏
```

### `ActiveUpstream` 关键字段

```go
type ActiveUpstream struct {
    ID                int64       // upstream_providers 表主键（绑定关系用同一 ID）
    BaseURL           *url.URL
    APIKeys           []string    // 已解密、仅含 enabled=1 的 Key
    KeyRowIDs         []int64     // 对应行 ID（与 APIKeys 一一对应）
    Name, ProxyURL    string
    ModelPatterns     []string    // 空 = 接受所有模型
    KeySchedulingMode string      // "round-robin" | "fill"
    // 内部：keyMu 保护 keyIndex / fillKeyIndex / fillKeyFailed
}
```

`NextAPIKey()` 返回 `(key, index, rowID)`；`MarkKeyFailed()` 在 fill 模式触发切换。

### `UpstreamProber`

```go
prober := proxy.NewUpstreamProber(store, dp, 30s, 5s)
go prober.Start(ctx)             // 立即探活一次 + ticker 循环
prober.ProbeNow()                 // admin 修改后立即触发
```

冷启动 / 运行时差异行为：
- 模型模式加载失败 + **已有活跃上游** → 保留旧快照（避免 fail-open）
- 模型模式加载失败 + **冷启动** → 降级为空模式（接受所有模型，确保启动）
- 全部上游 unhealthy → **保留上次活跃列表**，避免瞬时网络抖动 503 风暴

每次探活会调用 `store.AutoDisableFailingKeys(threshold)` 清理累计失败 Key。

### `BuildTransport(proxyURL string) (*http.Transport, error)`

按 `proxyURL` 字符串缓存（含空字符串 = 环境代理）。支持 `http`/`https`/`socks5`。`RemoveTransport` 从缓存删除并 `CloseIdleConnections()`。

## 关键依赖与配置

- `internal/store` — `UpstreamProber` 读取 `ListUpstreams` / `GetAllUpstreamModelPatterns` / `GetAllUpstreamAPIKeyRowIDs` / `AutoDisableFailingKeys`
- `golang.org/x/net/proxy` — SOCKS5 dialer
- 无外部 LLM SDK 依赖；纯 HTTP 协议级转发

### 上下文 Key（私有 struct，避免冲突）

| Key 类型 | 写入者 | 读取者 |
|---------|--------|-------|
| `allowedUpstreamIDsKey{}` | `middleware.UpstreamBindingMiddleware` | `DynamicProxy.ServeHTTP` |
| `keyModelOverridesKey{}` | `middleware.UpstreamBindingMiddleware` | `DynamicProxy.ServeHTTP` |

公共 helper：`ContextWithAllowedUpstreamIDs` / `AllowedUpstreamIDsFromContext` / `ContextWithKeyModelOverrides` / `KeyModelOverridesFromContext`。

### 响应头约定（与 audit 中间件契约）

`forwardResponse` 在 `WriteHeader` 前设置：
- `X-Upstream-Name` — 上游名
- `X-API-Key-Index` — 本次请求使用的 Key 索引（0-based）
- `X-Used-Proxy` — 实际经过的代理 URL（空 = 直连）
- `X-Model` — `ServeHTTP` 内更早设置（来自 body 提取）

`middleware.responseStatusCapture.WriteHeader` 会读取并 `Header().Del(...)`，不会泄漏到客户端。**不要修改这些头名而不同步更新 audit 中间件**。

### 安全相关

- **SSRF**：`UpstreamProber.probeUpstream` 与 admin 测试 client 都设 `CheckRedirect: ErrUseLastResponse`
- **请求体限流**：`maxBodySize = 32MB`（超出 → 413）
- **响应错误体限流**：`maxErrorBodySize = 256KB`
- **untrusted headers** 剥离：`X-Forwarded-*`、`CF-*`、`True-Client-IP` 等共 11 个；防止下游伪造身份给上游
- **响应敏感头剥离**：`Server`、`X-Request-Id`、`X-Oneapi-Request-Id`、`X-New-Api-Version` 等
- **错误体脱敏**：`sanitizeRules`：
  1. `[(sk|ak|rk|fk)-...]` → `[***]`
  2. `(request id: ...)` → 删除
  3. `RemainQuota = N` → `RemainQuota = ***`

## 扩展点

| 想做的事 | 修改位置 |
|---------|---------|
| 支持新协议风格 | `style.go` 加 const + `DetectProviderStyle` 加规则 + `extract.go` / `authrewrite.go` 加 case |
| 新增故障切换条件 | `dynamic.go` ServeHTTP 内 `if resp.StatusCode in {...}` 那段 |
| 新增 Key 调度策略 | `ActiveUpstream.NextAPIKey` switch case + `migrations.go` 兼容值 |
| 新增脱敏规则 | `sanitize.go` 的 `sanitizeRules` 数组追加（白名单式扩展） |
| 改请求体上限 | `dynamic.go` `maxBodySize` 常量 |
| 自定义探活逻辑 | `prober.go` `probeUpstream`（当前 HEAD `/v1/models`，500+ 视为不健康） |

## 测试与质量

| 测试文件 | 主要场景 |
|---------|---------|
| `dynamic_test.go` | 无活跃上游 → 503；正常代理；故障切换；body 缓冲；过滤逻辑 |
| `prober_test.go` | 探活循环、健康/不健康分类 |
| `transport_test.go` | 不同 scheme 缓存、SOCKS5 dialer |
| `extract_test.go` | OpenAI/Anthropic Key 提取与回退 |
| `style_test.go` | provider 风格判定矩阵 |
| `authrewrite_test.go` | 双向头改写 |
| `sanitize_test.go` | 各脱敏规则覆盖 |
| `binding_test.go` | filterUpstreams 行为 |
| `override_test.go` | matchModelOverrides 优先级（精确 > 最长通配） |

## 常见问题 (FAQ)

- **请求被 422 "no upstream available for model"**：模型不在任何上游的 `ModelPatterns` 中；要么给某上游加 pattern，要么清空（视为接受全部）
- **per-Key 覆盖命中后失败为何不回退默认路由**：设计如此（hard fail），防止配置失误时静默降级到不该用的上游；返回 422
- **响应里仍有 `[sk-xxx]`**：检查 `resp.StatusCode >= 400` 才走脱敏；2xx 响应不脱敏
- **同一代理 URL 的连接池为何不释放**：缓存按 `proxyURL` 字符串存活；`admin.tryRemoveTransport` 仅在没有其他上游引用时回收

## 相关文件清单

- `internal/proxy/dynamic.go`
- `internal/proxy/prober.go`
- `internal/proxy/transport.go`
- `internal/proxy/style.go`
- `internal/proxy/extract.go`
- `internal/proxy/authrewrite.go`
- `internal/proxy/sanitize.go`
- `internal/proxy/*_test.go`

## 变更记录 (Changelog)

- 2026-05-15 15:03:30：初始化模块文档
