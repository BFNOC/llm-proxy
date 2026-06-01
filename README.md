# LLM Proxy

轻量的自建 LLM 透明反向代理。下游统一访问 `/v1/...`，代理在运行时从 SQLite 管理的上游池里选择可用上游，并处理鉴权、模型路由、Key 调度、故障切换、审计日志和中文管理面板。

当前形态：一个 Go 二进制 + SQLite 数据库，无需 Redis、DynamoDB 或独立前端构建流程。

## 核心能力

- 统一 `/v1/...` 代理入口，自动识别 OpenAI / Anthropic 风格请求。
- Web 管理上游、下游 Key、模型路由、白名单、声明模型和运行设置。
- 每个上游支持多个 API Key，可单独启停、测试、复制，支持 `round-robin` / `fill` 调度。
- 下游 Key 可绑定指定上游，也可配置 per-key 模型路由覆盖。
- 支持上游独立代理地址：`http`、`https`、`socks5`。
- 支持无鉴权上游，适配公益站或本身不需要 API Key 的兼容服务。
- 上游失败时按候选上游故障切换，连续失败可自动禁用对应上游 Key。
- `/v1/models` 可合并上游真实模型和本地声明模型，并受模型白名单过滤。
- SSE 流式响应边读边写，不把流式结果缓冲到请求结束。
- 异步审计日志记录下游 Key、上游、上游 Key 索引、模型、代理、IP、状态码和延迟。

## 快速开始

```bash
make build

ENCRYPTION_KEY=01234567890123456789012345678901 \
ADMIN_TOKEN=my-secret-token \
./bin/llm-proxy
```

开发模式：

```bash
ENCRYPTION_KEY=01234567890123456789012345678901 \
ADMIN_TOKEN=my-secret-token \
make dev
```

启动后访问：

- 管理面板：`http://localhost:9002/admin/`
- 存活检查：`http://localhost:9002/healthz`
- 就绪检查：`http://localhost:9002/readyz`

`/healthz` 只表示进程存活；`/readyz` 会在没有健康上游时返回 `503`。

## 最小使用流程

### 1. 添加上游

```bash
curl -X POST http://localhost:9002/admin/api/upstreams \
  -H "Authorization: Bearer my-secret-token" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "openai-main",
    "base_url": "https://api.openai.com",
    "api_keys": ["sk-upstream-1", "sk-upstream-2"],
    "priority": 100,
    "key_scheduling_mode": "round-robin"
  }'
```

### 2. 创建下游 Key

```bash
curl -X POST http://localhost:9002/admin/api/keys \
  -H "Authorization: Bearer my-secret-token" \
  -H "Content-Type: application/json" \
  -d '{"name":"user-1","rpm_limit":60}'
```

### 3. 发起请求

OpenAI 风格：

```bash
curl http://localhost:9002/v1/chat/completions \
  -H "Authorization: Bearer sk-downstream..." \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"你好"}]}'
```

Anthropic 风格：

```bash
curl http://localhost:9002/v1/messages \
  -H "x-api-key: sk-downstream..." \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4-20250514","max_tokens":128,"messages":[{"role":"user","content":"你好"}]}'
```

## 常用配置

必要环境变量：

| 变量 | 说明 |
| ---- | ---- |
| `ENCRYPTION_KEY` | 32 字节原始字符串，或 64 位十六进制字符串，用于加密保存 Key |
| `ADMIN_TOKEN` | 管理面板和管理 API 的 Bearer token |

常用可选环境变量：

| 变量 | 说明 |
| ---- | ---- |
| `ENVIRONMENT` | 加载 `configs/{ENVIRONMENT}.yml` 覆盖 `configs/base.yml`，默认 `dev` |
| `PORT` | 覆盖监听端口 |
| `BIND_ADDR` | 监听地址，默认 `127.0.0.1` |
| `LOG_LEVEL` | `debug`、`info`、`warn`、`error` |
| `LOG_FORMAT` | `text` 或 `json` |
| `GEOIP_DB_PATH` | GeoLite2 City mmdb 路径，默认 `data/GeoLite2-City.mmdb` |

基础配置在 `configs/base.yml`，环境覆盖在 `configs/dev.yml`、`configs/staging.yml`、`configs/production.yml`。

## 文档

- [API 与管理端文档](docs/API.md)
- [命令列表](Makefile)

## 开发

```bash
make test
make fmt
make vet
go test ./...
```

常用源码入口：

- `cmd/llm-proxy/main.go`：启动、配置加载、中间件链和路由注册。
- `internal/admin`：管理 API 和内嵌 Web 面板。
- `internal/store`：SQLite schema、迁移和数据访问。
- `internal/proxy`：动态上游选择、认证头重写、故障切换和 transport 缓存。
- `internal/middleware`：鉴权、绑定、限流、审计、统计、模型过滤和流式刷新。

## 安全边界

- 上游 Key 和可复制下游 Key 使用 `ENCRYPTION_KEY` 做 AES-256-GCM 加密。
- 下游鉴权使用 Key 哈希，不用明文 Key 查询。
- 管理 API 会返回上游明文 Key，必须只暴露在可信网络或反向代理鉴权之后。
- 上游 `base_url` 会解析 DNS 并拒绝内网、loopback、link-local IP。
- 代理转发前会移除下游伪造身份相关请求头，并对上游错误响应做脱敏。
