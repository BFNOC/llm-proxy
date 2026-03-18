package store

import "time"

type UpstreamProvider struct {
	ID        int64
	Name      string
	BaseURL   string
	APIKey    string // encrypted at rest
	ProxyURL  string // 可选代理地址，支持 http/https/socks5，空表示继承环境代理
	Priority  int
	Enabled   bool   // persisted; disabled upstreams are skipped by the prober
	Healthy   bool   // runtime only, not persisted
	CreatedAt time.Time
	UpdatedAt time.Time
}

type DownstreamKey struct {
	ID        int64
	KeyHash   string
	KeyPrefix string
	Name      string
	RPMLimit  int
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type RequestLog struct {
	ID              int64
	DownstreamKeyID int64
	UpstreamName    string
	ClientIP        string
	ProviderStyle   string
	Path            string
	StatusCode      int
	LatencyMs       int64
	CreatedAt       time.Time
}

// ModelWhitelistEntry is a glob pattern for filtering /v1/models responses.
// If the whitelist is non-empty, only models matching at least one pattern are
// returned. Patterns support * wildcards (e.g. "claude-sonnet*"); patterns
// without wildcards match as substrings.
type ModelWhitelistEntry struct {
	ID        int64
	Pattern   string
	CreatedAt time.Time
}

// UpstreamModelPattern 表示某个上游支持的模型 glob 模式。
// 没有配置任何模式的上游视为支持所有模型（向后兼容）。
type UpstreamModelPattern struct {
	ID         int64
	UpstreamID int64
	Pattern    string
	CreatedAt  time.Time
}

