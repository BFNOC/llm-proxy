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

// UpstreamProber periodically probes all configured upstreams and updates the
// DynamicProxy to point at the highest-priority healthy one.
type UpstreamProber struct {
	store     *store.Store
	proxy     *DynamicProxy
	interval  time.Duration
	timeout   time.Duration
	currentID int64
	mu        sync.Mutex
}

// NewUpstreamProber creates a prober that checks upstreams on the given
// interval and uses timeout for each individual probe request.
func NewUpstreamProber(s *store.Store, p *DynamicProxy, interval, timeout time.Duration) *UpstreamProber {
	return &UpstreamProber{
		store:    s,
		proxy:    p,
		interval: interval,
		timeout:  timeout,
	}
}

// Start runs the probe loop until ctx is cancelled. It performs an initial
// probe immediately before entering the tick loop, so the proxy is usable as
// soon as Start returns (assuming it is called in a goroutine).
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

// probeOnce evaluates all upstreams and potentially switches the active one.
// It holds the mutex for the entire evaluation to prevent concurrent switches.
func (p *UpstreamProber) probeOnce() {
	p.mu.Lock()
	defer p.mu.Unlock()

	upstreams, err := p.store.ListUpstreams()
	if err != nil {
		slog.Error("prober: failed to list upstreams", "error", err)
		return
	}

	// Filter to only enabled upstreams.
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

	// Sort by priority ascending (lower value = higher preference).
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

	// Probe all enabled upstreams and collect the healthy ones.

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
		healthy = append(healthy, &ActiveUpstream{
			ID:            u.ID,
			BaseURL:       parsed,
			APIKey:        u.APIKey,
			Name:          u.Name,
			ProxyURL:      u.ProxyURL,
			ModelPatterns: allModelPatterns[u.ID],
		})
	}

	if len(healthy) == 0 {
		// No upstream is reachable. Keep the last active list rather than
		// clearing it, so transient network blips don't result in a 503 storm.
		slog.Error("prober: all enabled upstreams unhealthy, keeping last active")
		return
	}

	p.proxy.SetAllUpstreams(healthy)
	p.currentID = 0 // 多上游模式下不再维护“唯一当前上游”语义，保留该字段仅兼容旧接口
	slog.Info("prober: updated upstream list", "healthy_count", len(healthy))
}

// probeUpstream issues a HEAD request to baseURL/v1/models (optionally through
// the configured proxy) and returns true when the server is reachable (any
// HTTP status below 500 counts, including 401 which still means the server is up).
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

// ProbeNow triggers an immediate probe cycle. Useful after admin mutations.
func (p *UpstreamProber) ProbeNow() {
	p.probeOnce()
}

// GetCurrentID returns the ID of the upstream that is currently active.
// Returns 0 if no upstream has been selected yet.
func (p *UpstreamProber) GetCurrentID() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentID
}
