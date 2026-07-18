package proxy

import (
	"net/url"
	"sync"
	"sync/atomic"
)

// CircuitBreakerChecker 是熔断器的接口抽象，避免 proxy→middleware 的循环依赖。
// middleware.CircuitBreaker 实现了此接口。
type CircuitBreakerChecker interface {
	// IsAvailable 判断上游是否可以接受请求。
	IsAvailable(upstreamID int64) bool
	// RecordSuccess 记录成功请求。
	RecordSuccess(upstreamID int64)
	// RecordFailure 记录失败请求。
	RecordFailure(upstreamID int64, threshold, recoverySeconds int)
}

// UpstreamRPMChecker 是上游 RPM 限流器的接口抽象，避免 proxy→middleware 的循环依赖。
// middleware.UpstreamRPMLimiter 实现了此接口。
type UpstreamRPMChecker interface {
	// Allow 检查上游是否还有 RPM 配额。
	Allow(upstreamID int64, limit int) bool
}

// hopByHopHeaders 是不能被代理转发的 HTTP 逐跳头。
var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

// sensitiveUpstreamHeaders 是不应泄漏给下游客户端的上游响应头，
// 因为它们会暴露内部基础设施细节。
var sensitiveUpstreamHeaders = map[string]bool{
	"Server":           true,
	"X-Powered-By":     true,
	"Set-Cookie":       true,
	"Www-Authenticate": true,
	"X-Request-Id":     true,
	"X-Amzn-Requestid": true,
	// new-api / one-api 特有的响应头，会暴露上游平台版本和内部请求 ID
	"X-Oneapi-Request-Id": true,
	"X-New-Api-Version":   true,
	"X-Openai-Request-Id": true,
}

// untrustedRequestHeaders 是客户端提供的、转发给上游前应剥离的请求头，
// 以防止身份伪造。
var untrustedRequestHeaders = []string{
	"X-Forwarded-For",
	"X-Forwarded-Proto",
	"X-Forwarded-Host",
	"X-Real-IP",
	"Forwarded",
	"CF-Connecting-IP",
	"CF-IPCountry",
	"CF-Ray",
	"CF-Visitor",
	"True-Client-IP",
	"X-Client-IP",
	"X-Cluster-Client-IP",
}

var streamBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

// DynamicProxy 是支持多上游故障切换的反向代理。
// 健康的上游按优先级顺序依次尝试。遇到鉴权错误（401/403）、限流或额度耗尽
// （429）、网关错误（502/503/504）时会尝试下一个上游。
// 鉴权/额度/限流失败会使 Key 的连续失败计数递增；5xx 不会。
// 每个上游通过 BuildTransport 获取对应代理的 *http.Transport，相同代理复用连接池。
type DynamicProxy struct {
	allUpstreams   atomic.Value // 存储 []*ActiveUpstream
	activeRequests atomic.Int64

	// AutoDisableThreshold 连续 429 达到此值立即禁用 Key，0 表示禁用此功能。
	// 使用 atomic 读写，支持运行时动态修改。
	AutoDisableThreshold atomic.Int64

	// WhitelistMatcher 检查某个模型是否在全局白名单中。
	// 由 main.go 注入，以避免 proxy->middleware 的循环依赖。
	// 返回 true 表示允许该模型；nil 表示不启用白名单校验。
	WhitelistMatcher func(model string) bool

	// KeyFailCallback 在 API Key 请求失败时调用（429 或连接错误）。
	// 参数：upstreamID, keyRowID
	KeyFailCallback func(upstreamID, keyRowID int64)

	// KeySuccessCallback 在 API Key 请求成功时调用。
	// 参数：upstreamID, keyRowID
	KeySuccessCallback func(upstreamID, keyRowID int64)

	// CircuitBreaker 每上游熔断器，由 main.go 注入。
	CircuitBreaker CircuitBreakerChecker
	// UpstreamRPMLimiter 每上游 RPM 限流器，由 main.go 注入。
	UpstreamRPMLimiter UpstreamRPMChecker
}

// RateSnapshot 保存从上游响应头中提取的速率限制快照。
type RateSnapshot struct {
	RPMLimit     int    // x-ratelimit-limit-requests
	RPMUsed      int    // RPMLimit - RPMRemaining（已使用量）
	RPMRemaining int    // x-ratelimit-remaining-requests
	TPMLimit     int    // x-ratelimit-limit-tokens
	TPMRemaining int    // x-ratelimit-remaining-tokens
	ResetAt      string // x-ratelimit-reset-requests
}

// rateSnapshots 保存每个上游的速率限制快照，键为上游 ID。
var rateSnapshots sync.Map // map[int64]*RateSnapshot

// GetRateSnapshot 返回指定上游的速率限制快照（可能为 nil）。
func GetRateSnapshot(upstreamID int64) *RateSnapshot {
	v, ok := rateSnapshots.Load(upstreamID)
	if !ok {
		return nil
	}
	return v.(*RateSnapshot)
}

// GetAllRateSnapshots 返回所有上游的速率限制快照，用于管理面板展示。
func GetAllRateSnapshots() map[int64]*RateSnapshot {
	result := make(map[int64]*RateSnapshot)
	rateSnapshots.Range(func(key, value any) bool {
		result[key.(int64)] = value.(*RateSnapshot)
		return true
	})
	return result
}

// NewDynamicProxy 创建一个 DynamicProxy。
func NewDynamicProxy() *DynamicProxy {
	return &DynamicProxy{}
}

// SetAllUpstreams 原子地替换整个上游列表（已按优先级排序，最高优先级在前）。
// 当相同的上游 ID 仍然存在时，Key 调度游标（RR/fill 索引）会在探测器重建列表
// 时被保留下来。
func (dp *DynamicProxy) SetAllUpstreams(upstreams []*ActiveUpstream) {
	prev := dp.GetAllUpstreams()
	if len(prev) > 0 && len(upstreams) > 0 {
		byID := make(map[int64]*ActiveUpstream, len(prev))
		for _, u := range prev {
			if u != nil {
				byID[u.ID] = u
			}
		}
		for _, u := range upstreams {
			if u == nil {
				continue
			}
			old, ok := byID[u.ID]
			if !ok || old == nil {
				continue
			}
			old.keyMu.Lock()
			u.keyIndex = old.keyIndex
			u.fillKeyIndex = old.fillKeyIndex
			u.fillKeyFailed = old.fillKeyFailed
			old.keyMu.Unlock()
		}
	}
	dp.allUpstreams.Store(upstreams)
}

// SetActiveUpstream 是一个便捷方法，设置只含单个元素的上游列表。
// 保留此方法是为了兼容现有调用方。
func (dp *DynamicProxy) SetActiveUpstream(baseURL *url.URL, apiKey, name string) {
	dp.SetAllUpstreams([]*ActiveUpstream{{BaseURL: baseURL, APIKeys: []string{apiKey}, Name: name}})
}

// ClearActiveUpstream 移除所有上游，使代理返回 503。
func (dp *DynamicProxy) ClearActiveUpstream() {
	dp.allUpstreams.Store(([]*ActiveUpstream)(nil))
}

// GetActiveUpstream 返回第一个（最高优先级）上游，若无则返回 nil。
func (dp *DynamicProxy) GetActiveUpstream() *ActiveUpstream {
	all := dp.GetAllUpstreams()
	if len(all) == 0 {
		return nil
	}
	return all[0]
}

// GetAllUpstreams 返回当前配置的所有上游。
func (dp *DynamicProxy) GetAllUpstreams() []*ActiveUpstream {
	v := dp.allUpstreams.Load()
	if v == nil {
		return nil
	}
	return v.([]*ActiveUpstream)
}

// ActiveRequests 返回当前正在处理的并发请求数（原子读取，零开销）。
func (dp *DynamicProxy) ActiveRequests() int64 {
	return dp.activeRequests.Load()
}
