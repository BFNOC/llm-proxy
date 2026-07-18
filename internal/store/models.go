package store

import "time"

type UpstreamProvider struct {
	ID                            int64
	Name                          string
	BaseURL                       string
	APIKeys                       []string // 读取时解密；在 upstream_api_keys 表中加密存储
	ProxyURL                      string   // 可选代理地址，支持 http/https/socks5，空表示继承环境代理
	Priority                      int
	Enabled                       bool       // 持久化字段；禁用的上游会被 prober 跳过
	KeySchedulingMode             string     // "round-robin"（默认）或 "fill"
	AuthMode                      string     // "api_key"（默认，x-api-key）或 "oauth"（Authorization: Bearer）
	Remark                        string     // 管理员备注（Key 来源、用途等）
	WebSocketEnabled              bool       `json:"websocket_enabled"`                // 是否允许 WebSocket 透传
	AutoDiscoverModels            bool       `json:"auto_discover_models"`             // 是否启用模型自动发现
	LastModelDiscovery            *time.Time `json:"last_model_discovery,omitempty"`   // 上次成功发现模型的时间
	UpstreamRPMLimit              int        `json:"upstream_rpm_limit"`               // 上游每分钟请求限制，0 表示不限制
	CircuitBreakerThreshold       int        `json:"circuit_breaker_threshold"`        // 连续失败多少次后触发熔断
	CircuitBreakerRecoverySeconds int        `json:"circuit_breaker_recovery_seconds"` // 熔断后恢复探测的间隔秒数
	DeletedAt                     *time.Time `json:"deleted_at,omitempty"`             // 软删除时间，非 nil 表示已删除
	Healthy                       bool       // 仅运行时使用，不持久化
	CreatedAt                     time.Time
	UpdatedAt                     time.Time
}

// APIKeyInfo 表示单个 API Key 及其启用状态，用于管理面板展示。
type APIKeyInfo struct {
	RowID            int64  // upstream_api_keys 表主键
	Key              string // 已解密的明文 Key
	Enabled          bool
	ConsecutiveFails int // 连续失败次数
}

type DownstreamKey struct {
	ID            int64
	KeyHash       string
	KeyPrefix     string
	Name          string
	RPMLimit      int
	MaxConcurrent int `json:"max_concurrent"` // 并发连接数限制，0 表示不限制
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
	RequestSize     int64 `json:"request_size"`  // 请求体大小（字节）
	ResponseSize    int64 `json:"response_size"` // 响应体大小（字节）
	CreatedAt       time.Time
	HasFullRecord   bool              `json:"has_full_record"`
	Detail          *RequestLogDetail `json:"-"` // 仅用于与轻量日志同事务写入
	// RetainedDetailBytes 由审计队列用于追踪尚未落库的完整正文内存，不写入数据库。
	RetainedDetailBytes int64 `json:"-"`
}

// RequestLogDetail 保存按运行时策略选中的完整请求记录。
// Header 已在进入存储层前完成脱敏；正文达到上限时通过 Truncated 字段明确标记。
type RequestLogDetail struct {
	RequestLogID          int64  `json:"request_log_id"`
	SessionID             string `json:"session_id"`
	SessionSource         string `json:"session_source"`
	SessionPreview        string `json:"session_preview"`
	ResponseID            string `json:"response_id"`
	ParentResponseID      string `json:"parent_response_id"`
	Method                string `json:"method"`
	RawQuery              string `json:"raw_query"`
	RequestHeadersJSON    string `json:"request_headers_json"`
	RequestBody           string `json:"request_body"`
	RequestBodyTruncated  bool   `json:"request_body_truncated"`
	ResponseHeadersJSON   string `json:"response_headers_json"`
	ResponseBody          string `json:"response_body"`
	ResponseBodyTruncated bool   `json:"response_body_truncated"`
	CaptureStatus         string `json:"capture_status"`
}

// FullRecordingConfig 表示全量记录运行时策略。
// AllKeys 独立保存“全部密钥”语义，避免指定密钥被删除后空列表意外扩大记录范围。
type FullRecordingConfig struct {
	Enabled          bool    `json:"enabled"`
	AllKeys          bool    `json:"all_keys"`
	DownstreamKeyIDs []int64 `json:"downstream_key_ids"`
}

// LogQuery 是管理端日志查询与导出的共享过滤条件。
type LogQuery struct {
	KeyID       int64
	SessionID   string
	SessionKeys []LogSessionKey
	From        time.Time
	To          time.Time
	Limit       int
	FullOnly    bool
	StatusCode  int
	Model       string
	Path        string
}

// LogSessionKey 精确标识一个下游 Key 下的会话，避免相同 session_id 跨 Key 混合。
type LogSessionKey struct {
	DownstreamKeyID int64
	SessionID       string
}

// RequestLogSession 汇总一个下游 Key 下的连续会话。
type RequestLogSession struct {
	SessionID       string    `json:"session_id"`
	SessionSource   string    `json:"session_source"`
	SessionPreview  string    `json:"session_preview"`
	DownstreamKeyID int64     `json:"downstream_key_id"`
	FirstAt         time.Time `json:"first_at"`
	LastAt          time.Time `json:"last_at"`
	RequestCount    int       `json:"request_count"`
	ErrorCount      int       `json:"error_count"`
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

// UpstreamRateInfo 表示从上游响应头观测到的速率限制信息。
// 用于速率感知路由，优先选择余量充裕的上游。
type UpstreamRateInfo struct {
	UpstreamID      int64      `json:"upstream_id"`
	RPMLimit        int        `json:"rpm_limit"`
	RPMRemaining    int        `json:"rpm_remaining"`
	TPMLimit        int        `json:"tpm_limit"`
	TPMRemaining    int        `json:"tpm_remaining"`
	ResetAt         *time.Time `json:"reset_at,omitempty"`
	Last429At       *time.Time `json:"last_429_at,omitempty"`
	Consecutive429s int        `json:"consecutive_429s"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// UpstreamTemplate 表示预置的上游配置模板，用于快速添加常见 LLM 提供商。
type UpstreamTemplate struct {
	Name          string   `json:"name"`
	BaseURL       string   `json:"base_url"`
	AuthMode      string   `json:"auth_mode"`
	ModelPatterns []string `json:"model_patterns"`
}
