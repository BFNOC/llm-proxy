# API 与管理端文档

本文档记录 LLM Proxy 的管理 API、代理规则和运行时行为。所有 `/admin/api/...` 接口都需要：

```http
Authorization: Bearer {ADMIN_TOKEN}
```

## 上游管理

### 创建上游

```bash
curl -X POST http://localhost:9002/admin/api/upstreams \
  -H "Authorization: Bearer my-secret-token" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "openai-main",
    "base_url": "https://api.openai.com",
    "api_keys": ["sk-upstream-1", "sk-upstream-2"],
    "proxy_url": "socks5://127.0.0.1:7897",
    "priority": 100,
    "key_scheduling_mode": "round-robin",
    "remark": "主账号"
  }'
```

字段说明：

- `base_url` 支持 `http` / `https`，并拒绝解析到 private、loopback、link-local IP。
- `api_key` 和 `api_keys` 都可用；`api_key` 是兼容旧格式的单 Key 字段。
- `api_keys` 可为空，表示转发到无鉴权上游。
- `proxy_url` 可为空；为空时使用环境代理。非空时支持 `http`、`https`、`socks5`。
- `key_scheduling_mode` 支持 `round-robin` 和 `fill`。

## 模型路由

### 上游模型模式

```bash
curl -X PUT http://localhost:9002/admin/api/upstreams/1/models \
  -H "Authorization: Bearer my-secret-token" \
  -H "Content-Type: application/json" \
  -d '{"patterns":["gpt-*","o*"]}'
```

规则：

- 支持 `*` 和 `?` 通配。
- 未配置任何模式的上游视为支持所有模型。
- 请求模型没有任何可用上游时返回 `422 model_not_available`。

### 下游 Key 绑定上游

```bash
curl -X PUT http://localhost:9002/admin/api/keys/1/upstreams \
  -H "Authorization: Bearer my-secret-token" \
  -H "Content-Type: application/json" \
  -d '{"upstream_ids":[1,2]}'
```

绑定为空表示清空显式绑定，回到默认健康上游池。若绑定存在但当前没有任何绑定上游健康，代理返回 `403`，不会回退到未授权上游。

### Per-Key 模型覆盖

```bash
curl -X PUT http://localhost:9002/admin/api/keys/1/model-overrides \
  -H "Authorization: Bearer my-secret-token" \
  -H "Content-Type: application/json" \
  -d '{
    "overrides": [
      {"model_pattern":"claude-opus-*","upstream_id":2},
      {"model_pattern":"claude-opus-*","upstream_id":3}
    ]
  }'
```

匹配优先级为精确匹配优先，其次选择最长的通配模式。覆盖规则命中但目标上游不可用时返回 `422 override_upstream_unavailable`，不会回退默认路由。

### 模型白名单

白名单同时影响两个路径：

- `/v1/models` 返回结果只保留白名单内模型。
- 普通请求的 `model` 不在白名单内时返回 `403 model_not_allowed`。

未配置白名单时允许所有模型。

### 声明模型

```bash
curl -X PUT http://localhost:9002/admin/api/upstreams/1/declared-models \
  -H "Authorization: Bearer my-secret-token" \
  -H "Content-Type: application/json" \
  -d '{"models":["gpt-4o","gpt-4o-mini"]}'
```

`GET /v1/models` 会把上游真实响应和声明模型合并去重；如果上游返回 `404`、`502` 或 `501`，且存在声明模型，代理会合成 OpenAI 风格模型列表。

## 故障切换规则

- 没有任何健康上游：返回 `503 no active upstream available`。
- 下游 Key 绑定了上游，但绑定集合内无健康上游：返回 `403`。
- 请求模型没有匹配上游：返回 `422 model_not_available`。
- Per-Key 模型覆盖命中，但覆盖上游不可用：返回 `422 override_upstream_unavailable`。
- 非最后一个候选上游返回 `429`、`401`、`403`，或发生连接错误：尝试下一个上游。
- 最后一个候选仍失败：返回上游响应或通用 `502 bad gateway`。

启用连续失败自动停 Key 时，`429`、`401`、`403` 和连接错误会累计对应上游 Key 的失败次数；成功响应会清零。阈值来自 `upstream.auto_disable_threshold`，也可通过管理端设置动态修改。

## 上游测试与工具

管理面板和 API 提供以下能力：

- 测试上游代理连通性：`POST /admin/api/upstreams/{id}/test-proxy`
- 查询 new-api 风格额度：`POST /admin/api/upstreams/{id}/check-quota`
- 测试指定上游 Key：`POST /admin/api/upstreams/{id}/apikeys/{key_id}/test`
- 管理常用测试模型：`/admin/api/test-models`
- 查看请求日志：`GET /admin/api/logs`
- 查看 Key 使用统计：`GET /admin/api/logs/key-stats`
- 查看运行状态和连接池：`GET /admin/api/status`
- 修改运行时设置：`GET/PUT /admin/api/settings`

上游 Key 测试支持 `openai`、`anthropic`、`responses` 三种协议，并支持传入 Cloudflare `cf_clearance` 与 `User-Agent` 辅助排查受保护上游。

## 代理链路

```text
客户端请求
  |
  v
CORSMiddleware
  |
StatsMiddleware                 全局 RPM / RPS 统计
  |
RequestClassifier               识别 OpenAI / Anthropic 风格并提取下游 Key
  |
KeyResolver                     校验下游 Key，加载 Key ID 和 RPM 限制
  |
UpstreamBinding                 读取 Key 绑定上游和 per-key 模型覆盖
  |
PerKeyStatsMiddleware           记录已鉴权 Key 的实时请求统计
  |
RateLimitMiddleware             滑动窗口 RPM 限流
  |
AuditLogMiddleware              捕获状态码、上游名、模型、代理地址并异步落库
  |
StreamingMiddleware             SSE 响应即时 flush
  |
ModelFilterMiddleware           处理 /v1/models 的声明模型和白名单过滤
  |
DynamicProxy                    选择上游，重写认证头，按错误切换候选上游
```

后台 `UpstreamProber` 会周期性探测启用的上游，并把健康上游快照写入 `DynamicProxy`。请求路径只使用这个内存快照，不在热路径中读取数据库上游表。

## 管理 API 列表

### 上游

| 方法 | 路径 | 说明 |
| ---- | ---- | ---- |
| `GET` | `/admin/api/upstreams` | 列出上游，包含明文上游 Key 详情 |
| `POST` | `/admin/api/upstreams` | 创建上游 |
| `PUT` | `/admin/api/upstreams/{id}` | 编辑上游 |
| `DELETE` | `/admin/api/upstreams/{id}` | 删除上游 |
| `POST` | `/admin/api/upstreams/{id}/test-proxy` | 测试上游 `/v1/models` |
| `POST` | `/admin/api/upstreams/{id}/check-quota` | 查询 new-api 额度 |
| `GET` | `/admin/api/upstreams/models` | 批量获取上游模型模式 |
| `GET` | `/admin/api/upstreams/{id}/models` | 获取单个上游模型模式 |
| `PUT` | `/admin/api/upstreams/{id}/models` | 设置单个上游模型模式 |
| `GET` | `/admin/api/upstreams/declared-models` | 批量获取声明模型 |
| `GET` | `/admin/api/upstreams/{id}/declared-models` | 获取单个上游声明模型 |
| `PUT` | `/admin/api/upstreams/{id}/declared-models` | 设置单个上游声明模型 |

### 上游 Key

| 方法 | 路径 | 说明 |
| ---- | ---- | ---- |
| `GET` | `/admin/api/upstreams/{id}/apikeys` | 列出上游 Key |
| `POST` | `/admin/api/upstreams/{id}/apikeys` | 追加上游 Key |
| `DELETE` | `/admin/api/upstreams/{id}/apikeys/{key_id}` | 删除上游 Key |
| `PUT` | `/admin/api/upstreams/{id}/apikeys/{key_id}/enabled` | 启用或禁用上游 Key |
| `POST` | `/admin/api/upstreams/{id}/apikeys/{key_id}/test` | 测试指定上游 Key |

### 下游 Key

| 方法 | 路径 | 说明 |
| ---- | ---- | ---- |
| `GET` | `/admin/api/keys` | 列出下游 Key |
| `POST` | `/admin/api/keys` | 创建下游 Key |
| `PUT` | `/admin/api/keys/{id}` | 编辑名称、RPM 或启停 |
| `DELETE` | `/admin/api/keys/{id}` | 删除下游 Key |
| `GET` | `/admin/api/keys/{id}/reveal` | 复制已加密保存的明文 Key |
| `GET` | `/admin/api/keys/bindings` | 批量获取 Key 绑定 |
| `GET` | `/admin/api/keys/{id}/upstreams` | 获取单 Key 绑定 |
| `PUT` | `/admin/api/keys/{id}/upstreams` | 设置单 Key 绑定 |
| `GET` | `/admin/api/keys/model-overrides` | 批量获取模型覆盖 |
| `GET` | `/admin/api/keys/{id}/model-overrides` | 获取单 Key 模型覆盖 |
| `PUT` | `/admin/api/keys/{id}/model-overrides` | 设置单 Key 模型覆盖 |

### 日志、模型、设置

| 方法 | 路径 | 说明 |
| ---- | ---- | ---- |
| `GET` | `/admin/api/logs` | 查询请求日志 |
| `GET` | `/admin/api/logs/key-stats` | 查询 Key 使用统计 |
| `GET` | `/admin/api/models/whitelist` | 列出模型白名单 |
| `POST` | `/admin/api/models/whitelist` | 添加模型白名单 |
| `DELETE` | `/admin/api/models/whitelist/{id}` | 删除单条白名单 |
| `DELETE` | `/admin/api/models/whitelist/batch` | 批量删除白名单 |
| `GET` | `/admin/api/status` | 系统状态 |
| `GET` | `/admin/api/key-rpm` | 实时 Key RPM |
| `GET` | `/admin/api/test-models` | 列出测试模型 |
| `POST` | `/admin/api/test-models` | 创建测试模型 |
| `PUT` | `/admin/api/test-models/{id}` | 更新测试模型 |
| `DELETE` | `/admin/api/test-models/{id}` | 删除测试模型 |
| `GET` | `/admin/api/settings` | 获取运行时设置 |
| `PUT` | `/admin/api/settings` | 更新运行时设置 |
