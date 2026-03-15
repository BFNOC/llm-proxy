package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
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

// AllowedUpstreamIDsFromContext 读取当前请求的上游访问白名单。
// 约定 nil 或空切片表示“没有显式绑定”，调用方应继续允许全部健康上游。
func AllowedUpstreamIDsFromContext(ctx context.Context) []int64 {
	v, _ := ctx.Value(allowedUpstreamIDsKey{}).([]int64)
	return v
}

// ActiveUpstream 保存当前可用的上游端点信息。
type ActiveUpstream struct {
	// ID 对应 upstream_providers 表主键，用于把运行时健康列表和持久化绑定关系做稳定关联。
	ID      int64
	BaseURL *url.URL
	APIKey  string
	Name    string
}

// DynamicProxy is a reverse proxy that supports 429-based failover across
// multiple upstreams. All healthy upstreams are stored and tried in priority
// order. If an upstream returns 429, the next one is attempted. Only when all
// upstreams return 429 is the response forwarded to the client.
type DynamicProxy struct {
	allUpstreams atomic.Value // stores []*ActiveUpstream
	transport    *http.Transport
}

// NewDynamicProxy creates a DynamicProxy with a pre-configured transport.
func NewDynamicProxy() *DynamicProxy {
	dp := &DynamicProxy{
		transport: newProxyTransport(),
	}
	return dp
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

// ServeHTTP 实现 http.Handler 接口。按优先级顺序尝试上游，
// 遇到 429 时自动故障切换到下一个。请求体会被缓冲一次用于重试。
//
// 如果请求上下文里带有允许访问的 upstream ID 集合，代理只会尝试这些健康上游。
// 过滤发生在真正发起 RoundTrip 之前，确保未授权上游不会收到任何请求。
func (dp *DynamicProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

		resp, err := dp.transport.RoundTrip(outReq)
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

// newProxyTransport returns an *http.Transport tuned for proxying LLM API
// requests, including long-running streaming responses.
func newProxyTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 3 * time.Minute,
		DisableCompression:    true,
	}
}

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

