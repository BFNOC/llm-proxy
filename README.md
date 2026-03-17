# LLM Proxy

<img height="250" alt="Screenshot 2025-09-08 at 10 10 08 AM" src="https://github.com/user-attachments/assets/5c6ecf7f-14bf-4d67-ba48-f250c80e3205" />

一个轻量的 LLM API 透明反向代理，用 Go 编写。通过统一的 `/v1/...` 端点转发请求到动态选择的上游服务商，自动处理认证头重写、健康探活与故障切换。

## 特性

- **透明代理**: 统一 `/v1/...` 端点，自动检测 OpenAI / Anthropic 请求风格
- **动态上游**: 管理多个上游服务商，按优先级自动探活和故障切换
- **密钥管理**: 生成下游 API Key（`sk-` 前缀），SHA-256 哈希存储，明文仅返回一次
- **RPM 限流**: 每个密钥独立的滑动窗口请求频率限制
- **审计日志**: 异步批量写入 SQLite，不阻塞代理请求
- **管理面板**: 内置中文 Web 管理界面 + JSON REST API
- **流式支持**: SSE 流式响应透传，零缓冲
- **安全**: AES-256-GCM 加密存储上游密钥，SSRF 防护，XSS 防护
- **单文件部署**: SQLite 持久化，无需 Redis / DynamoDB 等外部依赖

## 快速开始

```bash
# 编译
make build

# 运行（需设置环境变量）
ENCRYPTION_KEY=01234567890123456789012345678901 \
ADMIN_TOKEN=my-secret-token \
./bin/llm-proxy
```

或直接开发模式运行：

```bash
ENCRYPTION_KEY=01234567890123456789012345678901 \
ADMIN_TOKEN=my-secret-token \
make dev
```

启动后访问:
- 管理面板: http://localhost:9002/admin/
- 健康检查: http://localhost:9002/healthz
- 就绪检查: http://localhost:9002/readyz

## 使用流程

### 1. 添加上游服务商

```bash
curl -X POST http://localhost:9002/admin/api/upstreams \
  -H "Authorization: Bearer my-secret-token" \
  -H "Content-Type: application/json" \
  -d '{"name":"openai","base_url":"https://api.openai.com","api_key":"sk-xxx","priority":1}'
```

### 2. 创建下游密钥

```bash
curl -X POST http://localhost:9002/admin/api/keys \
  -H "Authorization: Bearer my-secret-token" \
  -H "Content-Type: application/json" \
  -d '{"name":"user-1","rpm_limit":60}'
# 返回 {"id":1,"key":"sk-a1b2c3...","name":"user-1","rpm_limit":60}
# ⚠️ 明文密钥仅显示一次，请立即保存
```

### 3. 发起请求

```bash
# OpenAI 风格
curl http://localhost:9002/v1/chat/completions \
  -H "Authorization: Bearer sk-a1b2c3..." \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"你好"}]}'

# Anthropic 风格
curl http://localhost:9002/v1/messages \
  -H "x-api-key: sk-a1b2c3..." \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"你好"}]}'
```

## 架构

```
客户端请求 (Bearer sk-xxx 或 x-api-key: sk-xxx)
    │
    ▼
 CORSMiddleware            ← 仅 /v1/ 路由
    │
 RequestClassifier         ← 检测 OpenAI/Anthropic 风格，提取密钥
    │
 KeyResolver               ← 原子快照查找密钥哈希，401 无效密钥
    │
 RateLimiter               ← 滑动窗口 RPM 限流，429 超限
    │
 AuthRewrite               ← 用上游密钥替换下游密钥
    │
 StreamingMiddleware        ← SSE 响应即时刷新
    │
 DynamicProxy              ← atomic.Value 读取活跃上游，转发请求
    │                           │
    ▼                           ▼ (异步)
 活跃上游                   AuditLogger → 批量写入 SQLite

                            UpstreamProber (后台)
                            → 定期健康检查，故障时自动切换
```

## 配置

### 环境变量

| 变量 | 必需 | 说明 |
|------|------|------|
| `ENCRYPTION_KEY` | 是 | 32 字节密钥（或 64 位十六进制），用于加密上游 API Key |
| `ADMIN_TOKEN` | 是* | 管理接口认证令牌（admin 启用时必需） |
| `ENVIRONMENT` | 否 | 环境名（dev/staging/production），加载对应配置文件 |
| `PORT` | 否 | 监听端口，覆盖配置文件 |
| `LOG_LEVEL` | 否 | 日志级别（debug/info/warn/error） |
| `LOG_FORMAT` | 否 | 日志格式（text/json） |

### YAML 配置

基础配置 `configs/base.yml`：

```yaml
server:
  port: 9002

storage:
  sqlite_path: "./data/llm-proxy.db"

admin:
  enabled: true

upstream:
  probe_interval_seconds: 30
  probe_timeout_seconds: 5

audit:
  enabled: true
  batch_size: 100
  flush_interval_ms: 1000
  channel_buffer: 10000

logging:
  level: "info"
  format: "text"
```

环境配置文件（`dev.yml` / `staging.yml` / `production.yml`）覆盖基础配置。

## 管理 API

所有接口需要 `Authorization: Bearer {ADMIN_TOKEN}` 认证。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/admin/api/upstreams` | 列出上游（隐藏 API Key） |
| POST | `/admin/api/upstreams` | 添加上游 |
| PUT | `/admin/api/upstreams/{id}` | 编辑上游 |
| DELETE | `/admin/api/upstreams/{id}` | 删除上游 |
| GET | `/admin/api/keys` | 列出下游密钥 |
| POST | `/admin/api/keys` | 创建密钥（返回明文仅一次） |
| PUT | `/admin/api/keys/{id}` | 编辑密钥（名称/RPM/启停） |
| DELETE | `/admin/api/keys/{id}` | 删除密钥 |
| GET | `/admin/api/logs` | 查询请求日志 |
| GET | `/admin/api/status` | 系统状态 |

## Docker

```bash
# 开发模式
make docker-build
make docker-run

# 生产模式
make docker-build-prod
make docker-compose-prod
```

## 开发

```bash
make help          # 查看所有命令
make test          # 运行测试
make fmt           # 格式化代码
make vet           # 静态检查
make check         # 全部检查
```

## 依赖

- [gorilla/mux](https://github.com/gorilla/mux) - HTTP 路由
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) - 纯 Go SQLite（无 CGO）
- [testify](https://github.com/stretchr/testify) - 测试断言
- [yaml.v3](https://pkg.go.dev/gopkg.in/yaml.v3) - YAML 解析
