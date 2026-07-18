package proxy

import (
	"encoding/json"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

// newProxyTransport 已迁移到 transport.go 中的 BuildTransport / newBaseTransport。

// filterUpstreams 在不打乱原有优先级顺序的前提下，筛出当前请求允许访问的健康上游。
// 这里不重新排序，是为了让绑定逻辑只负责授权边界，不改变探测器决定的故障切换顺序。
func filterUpstreams(all []*ActiveUpstream, allowedIDs []int64) []*ActiveUpstream {
	// 先转成 set，避免对每个上游都线性扫描 allowedIDs。
	set := make(map[int64]bool, len(allowedIDs))
	for _, id := range allowedIDs {
		set[id] = true
	}
	var result []*ActiveUpstream
	for _, u := range all {
		if set[u.ID] {
			result = append(result, u)
		}
	}
	return result
}

// extractModelFromBody 从 JSON body 提取顶层 model 字段。
// 返回值: (model, isJSON)。isJSON 表示 body 是否为合法 JSON。
// 非 JSON 时 isJSON 为 false，调用方应跳过模型过滤。
// model 为非字符串类型（null、数字等）时视为无 model（isJSON=true, model=""）。
func extractModelFromBody(body []byte) (string, bool) {
	if len(body) == 0 {
		return "", false
	}
	var partial struct {
		Model json.RawMessage `json:"model"`
	}
	if err := json.Unmarshal(body, &partial); err != nil {
		return "", false // 非 JSON
	}
	if partial.Model == nil {
		return "", true // JSON 但无 model 字段
	}
	// 尝试解析为字符串；非字符串（null/数字/对象）时不报错，视为无可用 model
	var model string
	if err := json.Unmarshal(partial.Model, &model); err != nil {
		return "", true // model 存在但非字符串
	}
	return model, true
}

// filterUpstreamsByModel 按模型模式过滤上游列表。
// 没有配置模型模式的上游视为"支持所有模型"，始终保留。
// 使用 path.Match（而非 filepath.Match）避免 OS 路径分隔符差异。
func filterUpstreamsByModel(all []*ActiveUpstream, model string) []*ActiveUpstream {
	var result []*ActiveUpstream
	for _, u := range all {
		if len(u.ModelPatterns) == 0 {
			// 未配置模式 = 接受所有模型
			result = append(result, u)
			continue
		}
		for _, p := range u.ModelPatterns {
			if matched, _ := path.Match(p, model); matched {
				result = append(result, u)
				break
			}
		}
	}
	return result
}

// matchModelOverrides 按 per-key 覆盖规则匹配模型，返回应该使用的上游 ID 列表。
// 优先级：精确匹配 > 最具体的通配模式（按 pattern 长度降序）。
// 返回空切片表示没有匹配的覆盖规则。
func matchModelOverrides(overrides []KeyModelOverrideRule, model string) []int64 {
	// Phase 1: 优先找精确匹配（无通配符的 pattern）
	for _, o := range overrides {
		if !strings.Contains(o.ModelPattern, "*") && !strings.Contains(o.ModelPattern, "?") {
			if model == o.ModelPattern {
				return o.UpstreamIDs
			}
		}
	}

	// Phase 2: 找最具体（最长）的通配匹配
	var bestIDs []int64
	bestLen := -1
	for _, o := range overrides {
		if !strings.Contains(o.ModelPattern, "*") && !strings.Contains(o.ModelPattern, "?") {
			continue // 精确匹配的规则已在 Phase 1 处理
		}
		if matched, _ := path.Match(o.ModelPattern, model); matched {
			if len(o.ModelPattern) > bestLen {
				bestLen = len(o.ModelPattern)
				bestIDs = o.UpstreamIDs
			}
		}
	}
	return bestIDs
}

// parseRetryAfter 解析 Retry-After 响应头。
// 支持秒数格式（如 "2"）和 HTTP 日期格式（如 "Wed, 21 Oct 2015 07:28:00 GMT"）。
// 返回 0 表示头不存在或无法解析。
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	value = strings.TrimSpace(value)

	// 尝试解析为秒数
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}

	// 尝试解析为 HTTP 日期（RFC 1123）
	if t, err := time.Parse(time.RFC1123, value); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
		return 0
	}

	return 0
}

// captureRateHeaders 从上游响应头中提取速率限制信息并存入内存快照。
// 仅当响应头中包含速率限制相关的头时才更新。
func captureRateHeaders(h http.Header, upstreamID int64) {
	if upstreamID <= 0 {
		return
	}

	// 检查是否有任何速率限制头
	rpmLimit := h.Get("x-ratelimit-limit-requests")
	rpmRemaining := h.Get("x-ratelimit-remaining-requests")
	tpmLimit := h.Get("x-ratelimit-limit-tokens")
	tpmRemaining := h.Get("x-ratelimit-remaining-tokens")
	resetAt := h.Get("x-ratelimit-reset-requests")

	if rpmLimit == "" && rpmRemaining == "" && tpmLimit == "" && tpmRemaining == "" {
		return // 没有速率限制头，不更新
	}

	limit := atoiSafe(rpmLimit)
	remaining := atoiSafe(rpmRemaining)
	used := 0
	if limit > 0 && remaining >= 0 {
		used = limit - remaining
		if used < 0 {
			used = 0
		}
	}
	snap := &RateSnapshot{
		RPMLimit:     limit,
		RPMUsed:      used,
		RPMRemaining: remaining,
		TPMLimit:     atoiSafe(tpmLimit),
		TPMRemaining: atoiSafe(tpmRemaining),
		ResetAt:      resetAt,
	}
	rateSnapshots.Store(upstreamID, snap)
}

// atoiSafe 安全地把字符串转为整数，失败返回 0。
func atoiSafe(s string) int {
	if s == "" {
		return 0
	}
	n, _ := strconv.Atoi(s)
	return n
}
