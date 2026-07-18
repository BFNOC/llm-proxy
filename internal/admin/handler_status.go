package admin

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/Instawork/llm-proxy/internal/proxy"
)

// healthy_upstreams 取自 DynamicProxy 当前可用的健康列表，
// 而不是数据库静态配置，这样管理端看到的状态才和实际转发行为一致。

func (h *AdminHandler) getStatus(w http.ResponseWriter, r *http.Request) {
	var auditDropped int64
	if h.auditLogger != nil {
		auditDropped = h.auditLogger.DroppedCount()
	}

	// 健康上游列表
	type upstreamInfo struct {
		ID                int64  `json:"id"`
		Name              string `json:"name"`
		URL               string `json:"url"`
		KeyCount          int    `json:"key_count"`
		KeySchedulingMode string `json:"key_scheduling_mode"`
	}
	var healthyList []upstreamInfo
	if all := h.dynamicProxy.GetAllUpstreams(); len(all) > 0 {
		for _, u := range all {
			mode := u.KeySchedulingMode
			if mode == "" {
				mode = "round-robin"
			}
			healthyList = append(healthyList, upstreamInfo{
				ID: u.ID, Name: u.Name, URL: u.BaseURL.String(),
				KeyCount: len(u.APIKeys), KeySchedulingMode: mode,
			})
		}
	}
	// 固定返回空数组，避免前端在 null 和 [] 之间做额外分支。
	if healthyList == nil {
		healthyList = []upstreamInfo{}
	}

	// Key 总数
	// 统计信息采用尽力而为策略；即使计数失败，也不让状态接口整体不可用。
	keyCount, _ := h.store.CountKeys()

	// 当日请求数
	now := time.Now().UTC()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	todayRequests, _ := h.store.CountLogsSince(startOfDay)

	// 运行时长
	uptime := time.Since(startTime).Truncate(time.Second).String()

	status := map[string]interface{}{
		"healthy_upstreams": healthyList,
		"total_keys":        keyCount,
		"today_requests":    todayRequests,
		"audit_dropped":     auditDropped,
		"uptime":            uptime,
		"version":           h.version,
		"timestamp":         time.Now().UTC(),
		"active_requests":   h.dynamicProxy.ActiveRequests(),
	}

	// 实时 RPM/RPS 统计；计数器可能未初始化（单元测试场景），用尽力而为策略。
	if h.requestCounter != nil {
		status["rpm"] = h.requestCounter.RPM()
		status["rps"] = fmt.Sprintf("%.1f", h.requestCounter.RPS())
	} else {
		status["rpm"] = 0
		status["rps"] = "0.0"
	}

	// 连接池统计
	status["transport_pool"] = proxy.TransportPoolStats()

	// 熔断器状态
	if h.circuitBreaker != nil {
		cbStates := h.circuitBreaker.GetAllStates()
		cbMap := make(map[string]string, len(cbStates))
		for uid, state := range cbStates {
			cbMap[strconv.FormatInt(uid, 10)] = state.String()
		}
		status["circuit_breaker"] = cbMap
	}

	jsonOK(w, status)
}

// getKeyRPM 返回所有活跃 Key 的实时 RPM 数据。
// 拆分为独立端点，避免 /status 轮询时携带大量 per-key 数据。
func (h *AdminHandler) getKeyRPM(w http.ResponseWriter, r *http.Request) {
	if h.perKeyStats == nil {
		jsonOK(w, map[string]int{})
		return
	}
	jsonOK(w, h.perKeyStats.AllActiveRPMs())
}

// --- SSE 实时事件推送 ---

// activeSSEConns 限制并发 SSE 连接数，防止资源耗尽。
var activeSSEConns atomic.Int64

// sseEvents 通过 Server-Sent Events 向管理面板推送实时状态（RPM、RPS、活跃请求数、健康上游数）。
// 每 5 秒发送一次状态事件，每 15 秒发送一次心跳保活注释。
func (h *AdminHandler) sseEvents(w http.ResponseWriter, r *http.Request) {
	if activeSSEConns.Add(1) > 10 {
		activeSSEConns.Add(-1)
		jsonError(w, http.StatusTooManyRequests, "too many SSE connections")
		return
	}
	defer activeSSEConns.Add(-1)

	flusher, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	ctx := r.Context()
	statusTicker := time.NewTicker(5 * time.Second)
	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer statusTicker.Stop()
	defer heartbeatTicker.Stop()

	// 立即发送一次初始状态
	h.sendSSEStatus(w, flusher)

	for {
		select {
		case <-ctx.Done():
			return
		case <-statusTicker.C:
			h.sendSSEStatus(w, flusher)
		case <-heartbeatTicker.C:
			fmt.Fprintf(w, ":ping\n\n")
			flusher.Flush()
		}
	}
}

// sendSSEStatus 向 SSE 连接发送一条状态事件。
func (h *AdminHandler) sendSSEStatus(w http.ResponseWriter, flusher http.Flusher) {
	var rpm int
	var rps string
	if h.requestCounter != nil {
		rpm = h.requestCounter.RPM()
		rps = fmt.Sprintf("%.1f", h.requestCounter.RPS())
	} else {
		rpm = 0
		rps = "0.0"
	}

	healthyCount := len(h.dynamicProxy.GetAllUpstreams())

	payload := map[string]interface{}{
		"type":              "status",
		"rpm":               rpm,
		"rps":               rps,
		"active_requests":   h.dynamicProxy.ActiveRequests(),
		"healthy_upstreams": healthyCount,
		"timestamp":         time.Now().UTC(),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// refreshAllCaches 一键刷新所有内存缓存并触发探活。
func (h *AdminHandler) refreshAllCaches(w http.ResponseWriter, r *http.Request) {
	if err := h.keyCache.Reload(h.store); err != nil {
		slog.Error("admin: 刷新 key cache 失败", "error", err)
	}
	if h.overrideCache != nil {
		h.overrideCache.Reload()
	}
	if h.bindingCache != nil {
		h.bindingCache.Reload()
	}
	if h.modelFilter != nil {
		h.modelFilter.Reload()
		h.modelFilter.ReloadDeclaredModels()
	}
	go func() {
		defer func() { recover() }()
		h.prober.ProbeNow()
	}()
	slog.Info("admin: 全部缓存已刷新")
	jsonOK(w, map[string]interface{}{"status": "refreshed"})
}

// --- 延迟统计与健康历史 ---

// getLatencyStats 返回各上游的延迟统计（默认最近 24 小时）。
func (h *AdminHandler) getLatencyStats(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if v := r.URL.Query().Get("hours"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 {
			jsonError(w, http.StatusBadRequest, "invalid hours parameter")
			return
		}
		hours = parsed
	}
	if hours > 720 {
		hours = 720
	}
	stats, err := h.store.GetUpstreamLatencyStats(hours)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if stats == nil {
		stats = []map[string]interface{}{}
	}
	jsonOK(w, stats)
}

// getHealthHistory 返回指定上游的健康探测历史。
func (h *AdminHandler) getHealthHistory(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	hours := 24
	if v := r.URL.Query().Get("hours"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 {
			jsonError(w, http.StatusBadRequest, "invalid hours parameter")
			return
		}
		hours = parsed
	}
	if hours > 720 {
		hours = 720
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 {
			jsonError(w, http.StatusBadRequest, "invalid limit parameter")
			return
		}
		limit = parsed
	}
	history, err := h.store.GetHealthHistory(id, hours, limit)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	jsonOK(w, history)
}

// --- 上游速率信息 ---

// getUpstreamRateInfo 聚合实时速率快照与持久化的 429 历史。
func (h *AdminHandler) getUpstreamRateInfo(w http.ResponseWriter, r *http.Request) {
	// 从 DynamicProxy 获取实时 header 速率快照
	liveSnapshots := proxy.GetAllRateSnapshots()
	// 从 store 获取持久化的 429 历史
	persistedInfo, err := h.store.GetAllUpstreamRateInfo()
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// 合并两个来源的数据
	type rateInfo struct {
		UpstreamID      int64      `json:"upstream_id"`
		RPMLimit        int        `json:"rpm_limit,omitempty"`
		RPMUsed         int        `json:"rpm_used,omitempty"`
		RPMRemaining    int        `json:"rpm_remaining,omitempty"`
		Last429At       *time.Time `json:"last_429_at,omitempty"`
		Consecutive429s int        `json:"total_429_count,omitempty"`
	}

	merged := make(map[int64]*rateInfo)
	// 先填充持久化数据
	for _, p := range persistedInfo {
		merged[p.UpstreamID] = &rateInfo{
			UpstreamID:      p.UpstreamID,
			Last429At:       p.Last429At,
			Consecutive429s: p.Consecutive429s,
		}
	}
	// 叠加实时快照
	for uid, snap := range liveSnapshots {
		ri, ok := merged[uid]
		if !ok {
			ri = &rateInfo{UpstreamID: uid}
			merged[uid] = ri
		}
		ri.RPMLimit = snap.RPMLimit
		ri.RPMUsed = snap.RPMUsed
		ri.RPMRemaining = snap.RPMRemaining
	}

	result := make([]*rateInfo, 0, len(merged))
	for _, ri := range merged {
		result = append(result, ri)
	}
	jsonOK(w, result)
}

// --- 熔断状态 ---

// getCircuitStatus 返回所有上游的熔断器状态。
func (h *AdminHandler) getCircuitStatus(w http.ResponseWriter, r *http.Request) {
	if h.circuitBreaker == nil {
		jsonOK(w, map[string]string{})
		return
	}
	states := h.circuitBreaker.GetAllStates()
	result := make(map[string]string, len(states))
	for uid, state := range states {
		result[strconv.FormatInt(uid, 10)] = state.String()
	}
	jsonOK(w, result)
}
