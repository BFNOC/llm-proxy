package geoip

import (
	"log/slog"
	"net"
	"os"
	"strings"

	"github.com/oschwald/maxminddb-golang"
)

// GeoIP 使用 MaxMind mmdb 数据库（GeoLite2-City 或兼容格式）提供 IP 归属地查询。
// mmdb 文件通过内存映射加载，支持高性能并发安全查询。
type GeoIP struct {
	db *maxminddb.Reader
}

// mmdbRecord 对应 GeoLite2-City 的记录结构。
type mmdbRecord struct {
	Country struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Subdivisions []struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
}

// New 打开 mmdb 文件用于 IP 归属地查询。
// 当文件不存在或格式无效时返回 nil（而非 error），实现优雅降级。
// 调用方应在调用 Lookup 前判空。
func New(dbPath string) *GeoIP {
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		slog.Warn("geoip: mmdb 文件不存在，IP 归属地查询已禁用", "path", dbPath)
		return nil
	}

	db, err := maxminddb.Open(dbPath)
	if err != nil {
		slog.Error("geoip: 打开 mmdb 文件失败，IP 归属地查询已禁用", "path", dbPath, "error", err)
		return nil
	}

	slog.Info("geoip: mmdb 已加载", "path", dbPath, "type", db.Metadata.DatabaseType)
	return &GeoIP{db: db}
}

// Close 释放 mmdb 资源。对 nil 接收者安全调用。
func (g *GeoIP) Close() {
	if g != nil && g.db != nil {
		_ = g.db.Close()
	}
}

// Lookup 根据 IP 地址返回归属地字符串。
// 优先使用中文名称（zh-CN），无中文时回退到英文（en）。
// 格式："国家|省份|城市"。查询失败时返回空字符串。
func (g *GeoIP) Lookup(ip string) (result string) {
	if g == nil || len(ip) == 0 {
		return ""
	}

	// 防止 mmdb 库内部出现意外 panic。
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("geoip: lookup panic 已恢复", "ip", ip, "panic", r)
			result = ""
		}
	}()

	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}

	var record mmdbRecord
	err := g.db.Lookup(parsed, &record)
	if err != nil {
		return ""
	}

	return formatRecord(&record)
}

// formatRecord 将 mmdb 记录格式化为可读的归属地字符串。
// 优先选择 zh-CN 名称，无中文时回退到 en。
func formatRecord(r *mmdbRecord) string {
	var parts []string

	if name := pickName(r.Country.Names); name != "" {
		parts = append(parts, name)
	}
	for _, sub := range r.Subdivisions {
		if name := pickName(sub.Names); name != "" {
			parts = append(parts, name)
		}
	}
	if name := pickName(r.City.Names); name != "" {
		parts = append(parts, name)
	}

	return strings.Join(parts, "|")
}

// pickName 从多语言名称中选择中文（zh-CN），无中文时选英文（en）。
func pickName(names map[string]string) string {
	if name, ok := names["zh-CN"]; ok && name != "" {
		return name
	}
	if name, ok := names["en"]; ok && name != "" {
		return name
	}
	return ""
}
