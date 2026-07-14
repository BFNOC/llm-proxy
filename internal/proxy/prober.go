package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	store              *store.Store
	proxy              *DynamicProxy
	interval           time.Duration
	timeout            time.Duration
	currentID          int64
	lastHealthyCnt     int // 上次探活健康数，仅变化时打日志
	lastHealthCleanup  time.Time
	lastDiscoveryTime  time.Time // 上次执行模型自动发现的时间，控制 5 分钟间隔
	mu                 sync.Mutex
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
		ok, latencyMs, errMsg := p.probeUpstream(u.BaseURL, u.ProxyURL)
		// 记录探测结果到健康历史表
		if err := p.store.RecordHealthProbe(u.ID, ok, latencyMs, errMsg); err != nil {
			slog.Warn("prober: failed to record health probe", "upstream_id", u.ID, "error", err)
		}
		if !ok {
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

	// 每小时清理一次历史探测记录，保留最近 7 天
	if time.Since(p.lastHealthCleanup) > 1*time.Hour {
		if err := p.store.CleanHealthHistory(7); err != nil {
			slog.Warn("prober: failed to clean health history", "error", err)
		}
		p.lastHealthCleanup = time.Now()
	}

	// 模型自动发现阶段：每 5 分钟对启用了自动发现的健康上游执行一次
	if time.Since(p.lastDiscoveryTime) >= 5*time.Minute {
		healthyIDs := make(map[int64]bool, len(healthy))
		for _, h := range healthy {
			healthyIDs[h.ID] = true
		}
		for _, u := range enabled {
			if !u.AutoDiscoverModels || !healthyIDs[u.ID] {
				continue
			}
			if len(u.APIKeys) == 0 {
				continue
			}
			models, discoverErr := p.discoverModels(u.BaseURL, u.ProxyURL, u.APIKeys[0])
			if discoverErr != nil {
				slog.Warn("prober: model discovery failed, disabling auto-discover",
					"upstream_id", u.ID, "name", u.Name, "error", discoverErr)
				if disableErr := p.store.SetAutoDiscoverModels(u.ID, false); disableErr != nil {
					slog.Error("prober: failed to disable auto-discover", "upstream_id", u.ID, "error", disableErr)
				}
				continue
			}
			if len(models) > 0 {
				if updateErr := p.store.UpdateDiscoveredModels(u.ID, models); updateErr != nil {
					slog.Error("prober: failed to update discovered models", "upstream_id", u.ID, "error", updateErr)
				} else {
					slog.Info("prober: discovered models", "upstream_id", u.ID, "name", u.Name, "count", len(models))
				}
			}
		}
		p.lastDiscoveryTime = time.Now()
	}
}

// probeUpstream 向 baseURL/v1/models 发起 HEAD 请求（可选经由配置的代理），
// 返回 (healthy, latencyMs, errorMsg)。任何低于 500 的 HTTP 状态码都视为健康
// （包括 401，因为这仍然说明服务器是启动着的）。
func (p *UpstreamProber) probeUpstream(baseURL, proxyURL string) (bool, int64, string) {
	transport, err := BuildTransport(proxyURL)
	if err != nil {
		slog.Warn("prober: failed to build transport", "proxy", proxyURL, "error", err)
		return false, 0, fmt.Sprintf("build transport: %v", err)
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   p.timeout,
		// 禁止跟随重定向，防止 302 到内网地址的 SSRF 绕过
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	start := time.Now()
	resp, err := client.Head(baseURL + "/v1/models")
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		return false, latencyMs, err.Error()
	}
	resp.Body.Close()
	healthy := resp.StatusCode < 500
	var errMsg string
	if !healthy {
		errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return healthy, latencyMs, errMsg
}

// ProbeNow 立即触发一次探测周期。适用于 admin 修改配置之后的场景。
func (p *UpstreamProber) ProbeNow() {
	p.probeOnce()
}

// discoverModels 从上游 /v1/models 接口获取模型列表（OpenAI 格式）。
// 使用 GET 请求并以 Authorization: Bearer 鉴权，解析 {"data": [{"id": "..."}, ...]} 响应。
func (p *UpstreamProber) discoverModels(baseURL, proxyURL, apiKey string) ([]string, error) {
	transport, err := BuildTransport(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("build transport: %w", err)
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   p.timeout,
		// 禁止跟随重定向，防止 302 到内网地址的 SSRF 绕过
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequest("GET", baseURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// 限制响应体大小防止 OOM
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	models := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	return models, nil
}

// GetCurrentID 返回当前活跃上游的 ID。
// 如果尚未选定任何上游，则返回 0。
func (p *UpstreamProber) GetCurrentID() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentID
}
