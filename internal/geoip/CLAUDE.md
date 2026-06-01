[根目录](../../CLAUDE.md) > [internal](../) > **geoip**

# internal/geoip — IP 归属地查询

> 最后更新：2026-05-15 15:03:30

## 模块职责

通过内存映射加载 MaxMind `mmdb` 文件（GeoLite2-City 或兼容格式），为请求日志提供 IP → "国家|省份|城市" 字符串的查询。**优雅降级**：mmdb 不存在或损坏时 `New` 返回 `nil`，所有查询无操作。

## 入口与启动

唯一文件：`geoip.go`（~117 行）。

```go
geo := geoip.New("data/GeoLite2-City.mmdb")  // 缺失时返回 nil（已记日志）
defer geo.Close()                            // 对 nil 安全
region := geo.Lookup(ip)                     // nil 接收者返回 ""
```

由 `cmd/llm-proxy/main.go` 创建并注入到 `AuditLogger`；`AuditLogger.run()` 在写入 goroutine 中补 `RequestLog.IPRegion`，避免命中请求热路径。

## 对外接口

```go
type GeoIP struct { ... }   // 不导出字段

func New(dbPath string) *GeoIP            // 错误时返回 nil + 告警日志
func (*GeoIP) Close()                      // nil 安全
func (*GeoIP) Lookup(ip string) string    // "Country|Province|City" 或 ""
```

## 数据模型

`mmdbRecord` 仅取 `country.names` / `city.names` / `subdivisions[].names`。`pickName` 优先 `zh-CN`，回退 `en`。结果用 `|` 拼接非空段。

## 关键依赖与配置

- `github.com/oschwald/maxminddb-golang` v1.13.1
- 数据文件路径优先级：`GEOIP_DB_PATH` env > `data/GeoLite2-City.mmdb` 默认

## 安全与稳健性

- `Lookup` 内 `defer recover()`：mmdb 库内部 panic 不会击穿调用方
- IP 解析失败 → 返回 `""`，不报错
- 文件缺失 → 返回 `nil`，**仅告警**，不阻塞启动

## 扩展点

| 想做的事 | 修改位置 |
|---------|---------|
| 添加更多字段（ASN、经纬度） | 扩展 `mmdbRecord` 结构 + `formatRecord` |
| 改为查 IP 段缓存 | 在 `GeoIP` 加 LRU；当前每次查询 ~1µs 已足够 |
| 支持其他 mmdb（GeoIP2-Country） | 字段与 City 子集兼容，无需改代码 |

## 测试与质量

| 测试文件 | 覆盖 |
|---------|------|
| `geoip_test.go` | 文件不存在 → nil；nil 接收者安全；Lookup 各种异常输入 |

## 常见问题 (FAQ)

- **`/admin/api/logs` 返回的 `ip_region` 为空**：默认未携带 mmdb 文件；从 [MaxMind GeoLite2](https://www.maxmind.com/en/geolite2/signup) 免费下载并放到 `data/`
- **想用国内 ip2region 库**：本模块只读 mmdb；想换实现请保持 `Lookup(string) string` 接口签名

## 相关文件清单

- `internal/geoip/geoip.go`
- `internal/geoip/geoip_test.go`

## 变更记录 (Changelog)

- 2026-05-15 15:03:30：初始化模块文档
