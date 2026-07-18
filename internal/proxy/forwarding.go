package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

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

	// WebSocket 升级请求走独立的代理通道，不经过 body 缓冲和 HTTP 转发。
	if IsWebSocketUpgrade(r) {
		dp.serveWebSocket(w, r, upstreams)
		return
	}

	// 检测客户端使用的 provider 风格，用于鉴权头重写。
	style := DetectProviderStyle(r)

	// 缓冲请求体，以便在多个上游间重试时复用。
	// 限制为 32MB 以防止内存耗尽；LLM API 的请求体
	// 通常都是较小的 JSON 消息。
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

		// 将 model 写入响应头，供审计中间件记录到日志。
		if model != "" {
			w.Header().Set("X-Model", model)
		}

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

		// 熔断器检查：如果该上游处于熔断状态，跳过。
		if dp.CircuitBreaker != nil && !dp.CircuitBreaker.IsAvailable(active.ID) {
			slog.Debug("proxy: upstream circuit open, skipping",
				"upstream", active.Name, "upstream_id", active.ID)
			if isLast {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				json.NewEncoder(w).Encode(map[string]string{"error": "all upstreams circuit-broken"}) //nolint:errcheck
				return
			}
			continue
		}

		// 上游 RPM 限流检查：如果该上游已达到 RPM 上限，跳过。
		if active.UpstreamRPMLimit > 0 && dp.UpstreamRPMLimiter != nil {
			if !dp.UpstreamRPMLimiter.Allow(active.ID, active.UpstreamRPMLimit) {
				slog.Debug("proxy: upstream RPM limit exceeded, skipping",
					"upstream", active.Name, "upstream_id", active.ID,
					"limit", active.UpstreamRPMLimit)
				if isLast {
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Retry-After", "5")
					w.WriteHeader(http.StatusTooManyRequests)
					json.NewEncoder(w).Encode(map[string]string{"error": "all upstreams rate limited"}) //nolint:errcheck
					return
				}
				continue
			}
		}

		// 构建外发请求。
		outReq := r.Clone(r.Context())
		outReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		outReq.ContentLength = int64(len(bodyBytes))
		outReq.URL.Scheme = active.BaseURL.Scheme
		outReq.URL.Host = active.BaseURL.Host
		outReq.Host = active.BaseURL.Host

		// 拼接上游 base URL 中可能存在的路径前缀。
		if active.BaseURL.Path != "" && active.BaseURL.Path != "/" {
			outReq.URL.Path = strings.TrimRight(active.BaseURL.Path, "/") + outReq.URL.Path
		}

		// 为当前这个具体上游重写鉴权头（round-robin 选取 Key）。
		apiKey, keyIdx, keyRowID := active.NextAPIKey()
		RewriteAuthHeaders(outReq, style, apiKey, active.AuthMode)

		// 剥离不可信的代理/身份相关请求头，防止下游客户端
		// 在上游面前伪造自己的身份。
		for _, h := range untrustedRequestHeaders {
			outReq.Header.Del(h)
		}
		// RFC 7230 逐跳头（包括 Connection 头列出的 token 列表）。
		stripRequestHopByHop(outReq.Header)

		// 移除 Accept-Encoding，使上游通常返回纯文本
		//（Transport 上也设置了 DisableCompression）。模型过滤逻辑需要这样做。
		outReq.Header.Del("Accept-Encoding")

		// 按上游代理配置获取对应 transport。
		// OAuth 上游使用 Node/Claude Code TLS 指纹（utls），降低被识别为第三方客户端的概率。
		var upTransport *http.Transport
		var err error
		if active.AuthMode == AuthModeOAuth {
			upTransport, err = BuildTransportUTLS(active.ProxyURL)
		} else {
			upTransport, err = BuildTransport(active.ProxyURL)
		}
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
			// 网络错误：记录熔断器失败
			if dp.CircuitBreaker != nil {
				dp.CircuitBreaker.RecordFailure(active.ID,
					active.CircuitBreakerThreshold, active.CircuitBreakerRecoverySeconds)
			}
			if !isLast {
				active.MarkKeyFailed()
				if dp.KeyFailCallback != nil && keyRowID > 0 {
					dp.KeyFailCallback(active.ID, keyRowID)
				}
				slog.Warn("proxy: upstream transport error, trying next",
					"upstream", active.Name, "error", err)
				continue
			}
			// 最后一个上游也出错了 —— 向客户端返回通用 502。
			// 详细错误信息只记录在服务端日志中。
			slog.Error("proxy error", "error", err, "path", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "bad gateway"}) //nolint:errcheck
			return
		}

		// 故障切换：非最后一个上游遇到鉴权/限流/额度/网关错误。
		if shouldFailoverStatus(resp.StatusCode) && !isLast {
			var peek []byte
			if resp.StatusCode == http.StatusTooManyRequests {
				// 窥探一小段响应体以区分额度耗尽和临时限流；保留 Closer。
				peek = peekResponseBody(resp, 8<<10)
			}
			kind := classifyUpstreamFailure(resp.StatusCode, resp.Header, peek)

			// 智能 429 退避：如果 retry-after <= 3 秒，短暂等待后重试同一上游，
			// 避免不必要的故障切换。
			if resp.StatusCode == http.StatusTooManyRequests && kind == FailureRateLimit {
				if retryWait := parseRetryAfter(resp.Header.Get("Retry-After")); retryWait > 0 && retryWait <= 3*time.Second {
					resp.Body.Close()
					slog.Info("proxy: 429 with short retry-after, waiting before retry",
						"upstream", active.Name, "wait", retryWait)
					select {
					case <-r.Context().Done():
						return
					case <-time.After(retryWait):
					}
					// 重建请求并重试同一上游
					outReq2 := r.Clone(r.Context())
					outReq2.Body = io.NopCloser(bytes.NewReader(bodyBytes))
					outReq2.ContentLength = int64(len(bodyBytes))
					outReq2.URL.Scheme = active.BaseURL.Scheme
					outReq2.URL.Host = active.BaseURL.Host
					outReq2.Host = active.BaseURL.Host
					if active.BaseURL.Path != "" && active.BaseURL.Path != "/" {
						outReq2.URL.Path = strings.TrimRight(active.BaseURL.Path, "/") + r.URL.Path
					}
					RewriteAuthHeaders(outReq2, style, apiKey, active.AuthMode)
					for _, h := range untrustedRequestHeaders {
						outReq2.Header.Del(h)
					}
					stripRequestHopByHop(outReq2.Header)
					outReq2.Header.Del("Accept-Encoding")

					retryResp, retryErr := upTransport.RoundTrip(outReq2)
					if retryErr == nil && !shouldFailoverStatus(retryResp.StatusCode) {
						// 重试成功，走正常响应转发
						if dp.KeySuccessCallback != nil && keyRowID > 0 {
							dp.KeySuccessCallback(active.ID, keyRowID)
						}
						if dp.CircuitBreaker != nil {
							dp.CircuitBreaker.RecordSuccess(active.ID)
						}
						dp.forwardResponse(w, retryResp, active.Name, keyIdx, active.ProxyURL, active.ID)
						return
					}
					// 重试仍失败，按正常故障切换流程继续
					if retryErr == nil {
						retryResp.Body.Close()
					} else if dp.CircuitBreaker != nil {
						dp.CircuitBreaker.RecordFailure(active.ID,
							active.CircuitBreakerThreshold, active.CircuitBreakerRecoverySeconds)
					}
				}
			}

			resp.Body.Close()
			// 记录熔断器失败
			if dp.CircuitBreaker != nil {
				dp.CircuitBreaker.RecordFailure(active.ID,
					active.CircuitBreakerThreshold, active.CircuitBreakerRecoverySeconds)
			}
			// fill 模式下标记当前 Key 失败，下次调用切换到下一个 Key
			active.MarkKeyFailed()
			if shouldCountKeyFailure(kind) && dp.KeyFailCallback != nil && keyRowID > 0 {
				dp.KeyFailCallback(active.ID, keyRowID)
			}
			slog.Info("proxy: upstream returned error, trying next",
				"upstream", active.Name, "status", resp.StatusCode, "failure_kind", string(kind))
			continue
		}

		// 把响应转发给客户端。
		if resp.StatusCode < 400 {
			if dp.KeySuccessCallback != nil && keyRowID > 0 {
				dp.KeySuccessCallback(active.ID, keyRowID)
			}
			// 成功请求：重置熔断器
			if dp.CircuitBreaker != nil {
				dp.CircuitBreaker.RecordSuccess(active.ID)
			}
		} else if shouldFailoverStatus(resp.StatusCode) {
			// 最后一个上游（或非故障切换的代码路径）：仍然要跟踪 Key 健康状态。
			var peek []byte
			if resp.StatusCode == http.StatusTooManyRequests {
				peek = peekResponseBody(resp, 8<<10)
			}
			kind := classifyUpstreamFailure(resp.StatusCode, resp.Header, peek)
			active.MarkKeyFailed()
			if shouldCountKeyFailure(kind) && dp.KeyFailCallback != nil && keyRowID > 0 {
				dp.KeyFailCallback(active.ID, keyRowID)
			}
			// 最后一个上游失败：记录熔断器
			if dp.CircuitBreaker != nil {
				dp.CircuitBreaker.RecordFailure(active.ID,
					active.CircuitBreakerThreshold, active.CircuitBreakerRecoverySeconds)
			}
			slog.Info("proxy: upstream error on final candidate",
				"upstream", active.Name, "status", resp.StatusCode, "failure_kind", string(kind))
		}
		dp.forwardResponse(w, resp, active.Name, keyIdx, active.ProxyURL, active.ID)
		return
	}
}

// forwardResponse 把上游的 HTTP 响应拷贝给下游客户端，
// 处理 SSE 流式响应头及 flush。
func (dp *DynamicProxy) forwardResponse(w http.ResponseWriter, resp *http.Response, upstreamName string, keyIdx int, proxyURL string, upstreamID int64) {
	defer resp.Body.Close()

	// 提取并缓存上游的速率限制头快照（用于管理面板展示）。
	captureRateHeaders(resp.Header, upstreamID)

	// 拷贝响应头，过滤掉逐跳头和敏感头。
	for k, vv := range resp.Header {
		if hopByHopHeaders[k] || sensitiveUpstreamHeaders[k] {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	// 处理 SSE 相关的响应头。
	ct := resp.Header.Get("Content-Type")
	if ct == "text/event-stream" || strings.Contains(ct, "text/event-stream") {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.Header().Del("Content-Length")
	}

	// 把上游名称存入响应头供审计中间件使用。这个头会在 WriteHeader
	// 之前设置，审计中间件的 wrapper 会在响应到达客户端之前读取并删除它。
	w.Header().Set("X-Upstream-Name", upstreamName)
	w.Header().Set("X-API-Key-Index", strconv.Itoa(keyIdx))
	w.Header().Set("X-Used-Proxy", proxyURL)

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

	// 流式写出响应体，支持 SSE 场景下的 flush。
	if f, ok := w.(http.Flusher); ok {
		bufp := streamBufPool.Get().(*[]byte)
		buf := *bufp
		defer streamBufPool.Put(bufp)
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
