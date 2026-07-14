package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/Instawork/llm-proxy/internal/store"
)

// UpstreamProber 周期性探测所有已配置的上游，并更新 DynamicProxy，
// 使其指向优先级最高的健康上游。
type UpstreamProber struct {
	store          *store.Store
	proxy          *DynamicProxy
	interval       time.Duration
	timeout        time.Duration
	currentID      int64
	lastHealthyCnt int // 上次探活健康数，仅变化时打日志
	mu             sync.Mutex
}

// NewUpstreamProber 创建一个探测器，按给定的 interval 检查上游，
// 每次单独的探测请求使用 timeout 作为超时时间。
func NewUpstreamProber(s *store.Store, p *DynamicProxy, interval, timeout time.Duration) *UpstreamProber {
	return &UpstreamProber{
		store:    s,
		proxy:    p,
		interval: interval,
		timeout:  timeout,
	}
}

// Start 运行探测循环，直到 ctx 被取消。它会在进入 tick 循环之前立即执行一次
// 探测，因此只要在 goroutine 中调用 Start，代理就能尽快变得可用。
func (p *UpstreamProber) Start(ctx context.Context) {
	p.probeOnce()

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.probeOnce()
		}
	}
}

// probeOnce 评估所有上游，可能会切换当前活跃的上游。
// 整个评估过程会一直持有互斥锁，以防止并发切换。
func (p *UpstreamProber) probeOnce() {
	p.mu.Lock()
	defer p.mu.Unlock()

	upstreams, err := p.store.ListUpstreams()
	if err != nil {
		slog.Error("prober: failed to list upstreams", "error", err)
		return
	}

	// 只保留已启用的上游。
	var enabled []store.UpstreamProvider
	for _, u := range upstreams {
		if u.Enabled {
			enabled = append(enabled, u)
		}
	}

	if len(enabled) == 0 {
		p.proxy.ClearActiveUpstream()
		p.currentID = 0
		slog.Warn("prober: no enabled upstreams configured")
		return
	}

	// 按优先级升序排序（数值越小优先级越高）。
	sort.Slice(enabled, func(i, j int) bool {
		return enabled[i].Priority < enabled[j].Priority
	})

	// 批量加载所有上游的模型模式，避免在循环中对每个上游做单独查询。
	// 加载失败的两种策略：
	//   - 已有活跃上游（非冷启动）：放弃本轮更新，保持旧快照，避免 fail-open
	//   - 无活跃上游（冷启动）：降级为空模式（接受所有模型），确保服务能启动
	allModelPatterns, err := p.store.GetAllUpstreamModelPatterns()
	if err != nil {
		if existing := p.proxy.GetAllUpstreams(); len(existing) > 0 {
			slog.Error("prober: failed to load upstream model patterns, keeping last active list", "error", err)
			return
		}
		slog.Warn("prober: cold start - failed to load model patterns, proceeding without model routing", "error", err)
		allModelPatterns = make(map[int64][]string)
	}

	// 批量加载所有上游的 Key 行 ID
	allKeyRowIDs, err := p.store.GetAllUpstreamAPIKeyRowIDs()
	if err != nil {
		slog.Warn("prober: failed to load api key row ids", "error", err)
		allKeyRowIDs = make(map[int64][]int64)
	}

	// 自动禁用连续失败超过阈值的 Key
	threshold := int(p.proxy.AutoDisableThreshold.Load())
	if disabled, err := p.store.AutoDisableFailingKeys(threshold); err == nil && disabled > 0 {
		slog.Info("prober: auto-disabled failing keys", "count", disabled)
	}

	// 探测所有已启用的上游，收集健康的那些。

	var healthy []*ActiveUpstream
	for _, u := range enabled {
		if !p.probeUpstream(u.BaseURL, u.ProxyURL) {
			slog.Warn("prober: upstream unhealthy", "id", u.ID, "name", u.Name)
			continue
		}
		parsed, err := url.Parse(u.BaseURL)
		if err != nil {
			slog.Error("prober: invalid upstream URL", "url", u.BaseURL, "error", err)
			continue
		}
		// 把数据库里的 upstream ID、代理地址和模型模式带入运行时快照，
		// 后续代理过滤才能和 key_upstream_bindings 按同一主键精确匹配。
		authMode := u.AuthMode
		if authMode == "" {
			authMode = AuthModeAPIKey
		}
		healthy = append(healthy, &ActiveUpstream{
			ID:                u.ID,
			BaseURL:           parsed,
			APIKeys:           u.APIKeys,
			KeyRowIDs:         allKeyRowIDs[u.ID],
			Name:              u.Name,
			ProxyURL:          u.ProxyURL,
			ModelPatterns:     allModelPatterns[u.ID],
			KeySchedulingMode: u.KeySchedulingMode,
			AuthMode:          authMode,
			WebSocketEnabled:  u.WebSocketEnabled,
			keyFailures:       make(map[int]int64),
		})
	}

	if len(healthy) == 0 {
		// 没有可达的上游。保留上一次的活跃列表而不是清空它，
		// 这样瞬时网络抖动就不会导致 503 风暴。
		slog.Error("prober: all enabled upstreams unhealthy, keeping last active")
		return
	}

	p.proxy.SetAllUpstreams(healthy)
	p.currentID = 0 // 多上游模式下不再维护"唯一当前上游"语义，保留该字段仅兼容旧接口
	if len(healthy) != p.lastHealthyCnt {
		slog.Info("prober: updated upstream list", "healthy_count", len(healthy))
		p.lastHealthyCnt = len(healthy)
	}
}

// probeUpstream 向 baseURL/v1/models 发起 HEAD 请求（可选经由配置的代理），
// 当服务器可达时返回 true（任何低于 500 的 HTTP 状态码都算数，
// 包括 401，因为这仍然说明服务器是启动着的）。
func (p *UpstreamProber) probeUpstream(baseURL, proxyURL string) bool {
	transport, err := BuildTransport(proxyURL)
	if err != nil {
		slog.Warn("prober: failed to build transport", "proxy", proxyURL, "error", err)
		return false
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   p.timeout,
		// 禁止跟随重定向，防止 302 到内网地址的 SSRF 绕过
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Head(baseURL + "/v1/models")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

// ProbeNow 立即触发一次探测周期。适用于 admin 修改配置之后的场景。
func (p *UpstreamProber) ProbeNow() {
	p.probeOnce()
}

// GetCurrentID 返回当前活跃上游的 ID。
// 如果尚未选定任何上游，则返回 0。
func (p *UpstreamProber) GetCurrentID() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentID
}
