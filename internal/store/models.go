package store

import "time"

type UpstreamProvider struct {
	ID                 int64
	Name               string
	BaseURL            string
	APIKeys            []string // 读取时解密；在 upstream_api_keys 表中加密存储
	ProxyURL           string   // 可选代理地址，支持 http/https/socks5，空表示继承环境代理
	Priority           int
	Enabled            bool   // 持久化字段；禁用的上游会被 prober 跳过
	KeySchedulingMode  string // "round-robin"（默认）或 "fill"
	AuthMode           string // "api_key"（默认，x-api-key）或 "oauth"（Authorization: Bearer）
	Remark             string // 管理员备注（Key 来源、用途等）
	WebSocketEnabled   bool       `json:"websocket_enabled"`            // 是否允许 WebSocket 透传
	AutoDiscoverModels bool       `json:"auto_discover_models"`          // 是否启用模型自动发现
	LastModelDiscovery *time.Time `json:"last_model_discovery,omitempty"` // 上次成功发现模型的时间
	Healthy            bool       // 仅运行时使用，不持久化
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// APIKeyInfo 表示单个 API Key 及其启用状态，用于管理面板展示。
type APIKeyInfo struct {
	RowID              int64  // upstream_api_keys 表主键
	Key                string // 已解密的明文 Key
	Enabled            bool
	ConsecutiveFails   int // 连续失败次数
}

type DownstreamKey struct {
	ID            int64
	KeyHash       string
	KeyPrefix     string
	Name          string
	RPMLimit      int
	MaxConcurrent int  `json:"max_concurrent"` // 并发连接数限制，0 表示不限制
	Enabled       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type RequestLog struct {
	ID              int64
	DownstreamKeyID int64
	UpstreamName    string
	UpstreamKeyIdx  int    // 使用的上游 API Key 索引（0-based），-1 表示未知
	Model           string // 请求的模型名称
	UsedProxy       string // 使用的代理地址，空表示直连
	ClientIP        string
	IPRegion        string
	ProviderStyle   string
	Path            string
	StatusCode      int
	LatencyMs       int64
	RequestSize     int64  `json:"request_size"`  // 请求体大小（字节）
	ResponseSize    int64  `json:"response_size"` // 响应体大小（字节）
	CreatedAt       time.Time
}

// ModelWhitelistEntry 是用于过滤 /v1/models 响应的 glob 模式。
// 如果白名单非空，只返回匹配至少一个模式的模型。模式支持 * 通配符
// （例如 "claude-sonnet*"）；不带通配符的模式按子串匹配。
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

// KeyModelOverride 表示某个下游 Key 对特定模型的上游路由覆盖。
// 一个 key + 一个 model_pattern 可以对应多个 upstream_id（多行），支持 failover。
// 当请求匹配到覆盖规则时，优先使用精确匹配，否则按最具体的通配模式。
type KeyModelOverride struct {
	ID              int64
	DownstreamKeyID int64
	ModelPattern    string
	UpstreamID      int64
	CreatedAt       time.Time
}

// HealthRecord 表示一次上游健康探测记录。
type HealthRecord struct {
	ID           int64     `json:"id"`
	UpstreamID   int64     `json:"upstream_id"`
	Healthy      bool      `json:"healthy"`
	LatencyMs    int64     `json:"latency_ms"`
	ErrorMessage string    `json:"error_message"`
	CreatedAt    time.Time `json:"created_at"`
}

// TestModel 表示一个可复用的测试模型配置。
type TestModel struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Protocol  string    `json:"protocol"` // "openai", "anthropic", "responses"
	CreatedAt time.Time `json:"created_at"`
}

