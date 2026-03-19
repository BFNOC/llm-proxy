package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
)

// hopByHopHeaders are HTTP headers that must not be forwarded by proxies.
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

// sensitiveUpstreamHeaders are upstream response headers that should not be
// leaked to downstream clients as they expose internal infrastructure details.
var sensitiveUpstreamHeaders = map[string]bool{
	"Server":              true,
	"X-Powered-By":        true,
	"Set-Cookie":          true,
	"Www-Authenticate":    true,
	"X-Request-Id":        true,
	"X-Amzn-Requestid":    true,
	// new-api / one-api 特有的响应头，会暴露上游平台版本和内部请求 ID
	"X-Oneapi-Request-Id": true,
	"X-New-Api-Version":   true,
	"X-Openai-Request-Id": true,
}

// untrustedRequestHeaders are client-provided headers that should be stripped
// before forwarding to the upstream to prevent identity spoofing.
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

// allowedUpstreamIDsKey 使用私有空结构体作为 context key，
// 避免字符串 key 冲突，也避免外部代码通过同名字符串伪造绑定结果。
type allowedUpstreamIDsKey struct{}

// ContextWithAllowedUpstreamIDs 把当前请求允许访问的上游 ID 集合写入 context。
// 绑定关系使用稳定的数据库 ID，而不是名称或 URL，避免上游重命名后授权漂移。
func ContextWithAllowedUpstreamIDs(ctx context.Context, ids []int64) context.Context {
	return context.WithValue(ctx, allowedUpstreamIDsKey{}, ids)
}

// AllowedUpstreamIDsFromContext reads the upstream access whitelist.
func AllowedUpstreamIDsFromContext(ctx context.Context) []int64 {
	v, _ := ctx.Value(allowedUpstreamIDsKey{}).([]int64)
	return v
}

// ---------------------------------------------------------------------------
// Per-Key Model Override context helpers
// ---------------------------------------------------------------------------

type keyModelOverridesKey struct{}

// KeyModelOverrideRule is a runtime override rule.
// One ModelPattern can map to multiple UpstreamIDs (failover list).
type KeyModelOverrideRule struct {
	ModelPattern string
	UpstreamIDs  []int64
}

// ContextWithKeyModelOverrides writes per-key model routing overrides to context.
func ContextWithKeyModelOverrides(ctx context.Context, overrides []KeyModelOverrideRule) context.Context {
	return context.WithValue(ctx, keyModelOverridesKey{}, overrides)
}

// KeyModelOverridesFromContext reads per-key model routing overrides.
func KeyModelOverridesFromContext(ctx context.Context) []KeyModelOverrideRule {
	v, _ := ctx.Value(keyModelOverridesKey{}).([]KeyModelOverrideRule)
	return v
}

// ActiveUpstream 保存当前可用的上游端点信息。
type ActiveUpstream struct {
	// ID 对应 upstream_providers 表主键，用于把运行时健康列表和持久化绑定关系做稳定关联。
	ID            int64
	BaseURL       *url.URL
	APIKey        string
	Name          string
	ProxyURL      string   // 可选代理地址，空表示继承环境代理
	ModelPatterns []string // 支持的模型 glob 模式，空表示接受所有模型
}

// DynamicProxy is a reverse proxy that supports 429-based failover across
// multiple upstreams. All healthy upstreams are stored and tried in priority
// order. If an upstream returns 429, the next one is attempted. Only when all
// upstreams return 429 is the response forwarded to the client.
// 每个上游通过 BuildTransport 获取对应代理的 *http.Transport，相同代理复用连接池。
type DynamicProxy struct {
	allUpstreams    atomic.Value // stores []*ActiveUpstream
	activeRequests atomic.Int64

	// WhitelistMatcher checks if a model is in the global whitelist.
	// Injected from main.go to avoid proxy->middleware circular dependency.
	// Returns true if model is allowed; nil means no whitelist enforcement.
	WhitelistMatcher func(model string) bool
}

// NewDynamicProxy creates a DynamicProxy.
func NewDynamicProxy() *DynamicProxy {
	return &DynamicProxy{}
}

// SetAllUpstreams atomically replaces the full list of upstreams (sorted by
// priority, highest-priority first).
func (dp *DynamicProxy) SetAllUpstreams(upstreams []*ActiveUpstream) {
	dp.allUpstreams.Store(upstreams)
}

// SetActiveUpstream is a convenience method that sets a single-element upstream
// list. Kept for backward compatibility with existing callers.
func (dp *DynamicProxy) SetActiveUpstream(baseURL *url.URL, apiKey, name string) {
	dp.SetAllUpstreams([]*ActiveUpstream{{BaseURL: baseURL, APIKey: apiKey, Name: name}})
}

// ClearActiveUpstream removes all upstreams so the proxy returns 503.
func (dp *DynamicProxy) ClearActiveUpstream() {
	dp.allUpstreams.Store(([]*ActiveUpstream)(nil))
}

// GetActiveUpstream returns the first (highest-priority) upstream, or nil.
func (dp *DynamicProxy) GetActiveUpstream() *ActiveUpstream {
	all := dp.GetAllUpstreams()
	if len(all) == 0 {
		return nil
	}
	return all[0]
}

// GetAllUpstreams returns all currently configured upstreams.
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

// ServeHTTP 实现 http.Handler 接口。按优先级顺序尝试上游，
// 遇到 429 时自动故障切换到下一个。请求体会被缓冲一次用于重试。
//
// 如果请求上下文里带有允许访问的 upstream ID 集合，代理只会尝试这些健康上游。
// 过滤发生在真正发起 RoundTrip 之前，确保未授权上游不会收到任何请求。
func (dp *DynamicProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	dp.activeRequests.Add(1)
	defer dp.activeRequests.Add(-1)

	upstreams := dp.GetAllUpstreams()
	if len(upstreams) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "no active upstream available"}) //nolint:errcheck
		return
	}

	// 先按绑定关系裁剪健康上游列表；如果裁剪后为空，直接返回 403，保持 fail-closed。
	if allowed := AllowedUpstreamIDsFromContext(r.Context()); len(allowed) > 0 {
		filtered := filterUpstreams(upstreams, allowed)
		if len(filtered) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "no permitted upstream available for this key"}) //nolint:errcheck
			return
		}
		upstreams = filtered
	}

	// Detect provider style for auth rewriting.
	style := DetectProviderStyle(r)

	// Buffer request body for potential retries across upstreams.
	// Limit to 32MB to prevent memory exhaustion; LLM API payloads are
	// typically small JSON messages.
	const maxBodySize = 32 << 20 // 32 MB
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize+1))
	r.Body.Close()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to read request body"}) //nolint:errcheck
		return
	}
	if int64(len(bodyBytes)) > maxBodySize {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		json.NewEncoder(w).Encode(map[string]string{"error": "request body too large (max 32MB)"}) //nolint:errcheck
		return
	}

	// 从已缓冲的 body 提取 model 字段，按模型模式过滤上游。
	// GET 请求（如 /v1/models）不含 model 字段，跳过过滤。
	// 非 JSON body 也跳过过滤（可能是 multipart 等格式），由上游处理。
	var model string
	if r.Method != http.MethodGet {
		var isJSON bool
		model, isJSON = extractModelFromBody(bodyBytes)

		// 全局白名单请求拦截：校验 model 是否在白名单中。
		// 仅在白名单非空且 model 有效时执行校验。
		if isJSON && model != "" && dp.WhitelistMatcher != nil {
			if !dp.WhitelistMatcher(model) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
					"error": map[string]interface{}{
						"message": fmt.Sprintf("model %q is not allowed by the model whitelist", model),
						"type":    "invalid_request_error",
						"code":    "model_not_allowed",
					},
				})
				return
			}
		}

		if isJSON && model != "" {
			filtered := filterUpstreamsByModel(upstreams, model)
			if len(filtered) == 0 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
					"error": map[string]interface{}{
						"message": fmt.Sprintf("no upstream available for model: %s", model),
						"type":    "invalid_request_error",
						"code":    "model_not_available",
					},
				})
				return
			}
			upstreams = filtered
		}
	}

	// Per-key 模型路由覆盖：如果命中覆盖规则，强制使用指定上游。
	// 覆盖后无可用上游时 hard fail，不回退到默认路由。
	if model != "" {
		if overrides := KeyModelOverridesFromContext(r.Context()); len(overrides) > 0 {
			if overrideIDs := matchModelOverrides(overrides, model); len(overrideIDs) > 0 {
				filtered := filterUpstreams(upstreams, overrideIDs)
				if len(filtered) == 0 {
					slog.Warn("proxy: per-key model override matched but no upstream available",
						"model", model, "override_upstreams", overrideIDs)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnprocessableEntity)
					json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
						"error": map[string]interface{}{
							"message": fmt.Sprintf("override upstream for model %q is not available", model),
							"type":    "invalid_request_error",
							"code":    "override_upstream_unavailable",
						},
					})
					return
				}
				upstreams = filtered
			}
		}
	}

	for i, active := range upstreams {
		isLast := i == len(upstreams)-1

		// Build outgoing request.
		outReq := r.Clone(r.Context())
		outReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		outReq.ContentLength = int64(len(bodyBytes))
		outReq.URL.Scheme = active.BaseURL.Scheme
		outReq.URL.Host = active.BaseURL.Host
		outReq.Host = active.BaseURL.Host

		// Prepend any path prefix from the upstream base URL.
		if active.BaseURL.Path != "" && active.BaseURL.Path != "/" {
			outReq.URL.Path = strings.TrimRight(active.BaseURL.Path, "/") + outReq.URL.Path
		}

		// Rewrite auth headers for this specific upstream.
		RewriteAuthHeaders(outReq, style, active.APIKey)

		// Strip untrusted proxy/identity headers to prevent downstream
		// clients from spoofing their identity at the upstream.
		for _, h := range untrustedRequestHeaders {
			outReq.Header.Del(h)
		}

		// Remove Accept-Encoding so Go's transport handles decompression
		// transparently. This ensures response body is always plain text
		// for middleware processing (e.g. model filtering).
		outReq.Header.Del("Accept-Encoding")

		// 按上游代理配置获取对应 transport
		upTransport, err := BuildTransport(active.ProxyURL)
		if err != nil {
			if !isLast {
				slog.Warn("proxy: failed to build transport, trying next",
					"upstream", active.Name, "error", err)
				continue
			}
			slog.Error("proxy: failed to build transport", "error", err, "upstream", active.Name)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "bad gateway"}) //nolint:errcheck
			return
		}
		resp, err := upTransport.RoundTrip(outReq)
		if err != nil {
			if !isLast {
				slog.Warn("proxy: upstream transport error, trying next",
					"upstream", active.Name, "error", err)
				continue
			}
			// Last upstream also errored — return generic 502 to client.
			// Full error details are logged server-side only.
			slog.Error("proxy error", "error", err, "path", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "bad gateway"}) //nolint:errcheck
			return
		}

		// On 429 from a non-last upstream, try the next one.
		if resp.StatusCode == http.StatusTooManyRequests && !isLast {
			resp.Body.Close()
			slog.Info("proxy: upstream returned 429, trying next",
				"upstream", active.Name)
			continue
		}

		// Forward response to client.
		dp.forwardResponse(w, resp, active.Name)
		return
	}
}

// forwardResponse copies an upstream HTTP response to the downstream client,
// handling SSE streaming headers and flushing.
func (dp *DynamicProxy) forwardResponse(w http.ResponseWriter, resp *http.Response, upstreamName string) {
	defer resp.Body.Close()

	// Copy response headers, filtering out hop-by-hop and sensitive headers.
	for k, vv := range resp.Header {
		if hopByHopHeaders[k] || sensitiveUpstreamHeaders[k] {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	// Handle SSE headers.
	ct := resp.Header.Get("Content-Type")
	if ct == "text/event-stream" || strings.Contains(ct, "text/event-stream") {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.Header().Del("Content-Length")
	}

	// Store upstream name for audit middleware. This header is set BEFORE
	// WriteHeader and will be read then deleted by the audit middleware
	// wrapper before the response reaches the client.
	w.Header().Set("X-Upstream-Name", upstreamName)

	// 对非 2xx 错误响应体做脱敏：缓冲整个 body，执行正则替换后再写回客户端。
	// 这样可以隐藏上游令牌标识、请求 ID、额度数字等内部信息。
	// 正常 2xx 响应（含流式 SSE）仍走下方的流式转发路径，不受影响。
	if resp.StatusCode >= 400 {
		errBody, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
		// 排空剩余未读内容，确保 HTTP 连接可被 Transport 复用。
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		if err != nil {
			slog.Warn("proxy: failed to read error response body for sanitization",
				"upstream", upstreamName, "error", err)
			w.WriteHeader(resp.StatusCode)
			fmt.Fprintf(w, `{"error":{"message":"upstream error","type":"proxy_error"}}`) //nolint:errcheck
			return
		}
		sanitized := SanitizeErrorBody(errBody)
		w.Header().Set("Content-Length", strconv.Itoa(len(sanitized)))
		w.WriteHeader(resp.StatusCode)
		w.Write(sanitized) //nolint:errcheck
		return
	}

	w.WriteHeader(resp.StatusCode)

	// Stream body with flush support for SSE.
	if f, ok := w.(http.Flusher); ok {
		buf := make([]byte, 32*1024)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				w.Write(buf[:n]) //nolint:errcheck
				f.Flush()
			}
			if readErr != nil {
				break
			}
		}
	} else {
		io.Copy(w, resp.Body) //nolint:errcheck
	}
}

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
