package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/gorilla/websocket"
	xproxy "golang.org/x/net/proxy"
)

// testUpstreamProxy 通过上游配置的代理对其 base_url 发 GET /v1/models 请求，
// 携带 API Key 验证连通性并返回支持的模型列表。
func (h *AdminHandler) testUpstreamProxy(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 解析可选的 CF 绕过参数
	var cfOpts struct {
		CFClearance string `json:"cf_clearance"`
		CFUserAgent string `json:"cf_user_agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&cfOpts); err != nil && err.Error() != "EOF" {
		jsonError(w, http.StatusBadRequest, "invalid CF params JSON")
		return
	}

	upstream, err := h.store.GetUpstream(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("upstream %d not found", id))
		return
	}

	// 构造带代理的 HTTP client
	transport, err := proxy.BuildTransport(upstream.ProxyURL)
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("invalid proxy config: %v", err),
		})
		return
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		// 禁止跟随重定向，防止 302 到内网地址的 SSRF 绕过
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	testURL := strings.TrimRight(upstream.BaseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(r.Context(), "GET", testURL, nil)
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("构造请求失败: %v", err),
		})
		return
	}
	var firstKey string
	if len(upstream.APIKeys) > 0 {
		firstKey = upstream.APIKeys[0]
	}
	req.Header.Set("Authorization", "Bearer "+firstKey)
	applyCFHeaders(req, cfOpts.CFClearance, cfOpts.CFUserAgent)

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success":    false,
			"error":      err.Error(),
			"latency_ms": latency.Milliseconds(),
		})
		return
	}
	defer resp.Body.Close()

	// 限制读取 256KB
	body, err := io.ReadAll(io.LimitReader(resp.Body, 262144))
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success":     false,
			"error":       fmt.Sprintf("读取响应失败: %v", err),
			"status_code": resp.StatusCode,
			"latency_ms":  latency.Milliseconds(),
		})
		return
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		jsonOK(w, map[string]interface{}{
			"success":     false,
			"error":       fmt.Sprintf("上游返回 HTTP %d", resp.StatusCode),
			"status_code": resp.StatusCode,
			"latency_ms":  latency.Milliseconds(),
		})
		return
	}

	// 解析 OpenAI 风格的 /v1/models 响应
	var modelsResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	var models []string
	if err := json.Unmarshal(body, &modelsResp); err == nil && len(modelsResp.Data) > 0 {
		for _, m := range modelsResp.Data {
			if m.ID != "" {
				models = append(models, m.ID)
			}
		}
	}

	jsonOK(w, map[string]interface{}{
		"success":     true,
		"status_code": resp.StatusCode,
		"latency_ms":  latency.Milliseconds(),
		"models":      models,
	})
}

// testUpstreamWebSocket 通过 WebSocket 握手探测上游是否支持 Realtime API。
// 成功连接后立即发送关闭帧并断开，不产生实际对话流量。
func (h *AdminHandler) testUpstreamWebSocket(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	upstream, err := h.store.GetUpstream(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("upstream %d not found", id))
		return
	}

	// 将 http(s):// 替换为 ws(s)://，拼接 Realtime API 路径
	wsURL := strings.TrimRight(upstream.BaseURL, "/")
	if strings.HasPrefix(wsURL, "https://") {
		wsURL = "wss://" + strings.TrimPrefix(wsURL, "https://")
	} else if strings.HasPrefix(wsURL, "http://") {
		wsURL = "ws://" + strings.TrimPrefix(wsURL, "http://")
	}
	wsURL += "/v1/realtime?model=gpt-4o-realtime-preview"

	// 获取第一个启用的 API Key（无鉴权模式下仍尝试无 Key 连接）
	var authKey string
	// Realtime API 始终使用 Bearer 鉴权，无需区分 auth_mode
	keyInfos, err := h.store.GetUpstreamAllAPIKeys(id)
	if err == nil {
		for _, ki := range keyInfos {
			if ki.Enabled {
				authKey = ki.Key
				break
			}
		}
	}

	// 构造 WebSocket Dialer，支持上游代理（http/https/socks5）
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	if upstream.ProxyURL != "" {
		parsed, pErr := url.Parse(upstream.ProxyURL)
		if pErr != nil {
			jsonOK(w, map[string]interface{}{
				"websocket_supported": false,
				"message":             fmt.Sprintf("代理 URL 解析失败: %v", pErr),
			})
			return
		}
		switch parsed.Scheme {
		case "http", "https":
			dialer.Proxy = http.ProxyURL(parsed)
		case "socks5":
			socksDialer, sErr := xproxy.FromURL(parsed, xproxy.Direct)
			if sErr != nil {
				jsonOK(w, map[string]interface{}{
					"websocket_supported": false,
					"message":             fmt.Sprintf("SOCKS5 代理创建失败: %v", sErr),
				})
				return
			}
			if cd, ok := socksDialer.(xproxy.ContextDialer); ok {
				dialer.NetDialContext = cd.DialContext
			} else {
				dialer.NetDial = socksDialer.Dial
			}
		}
	}

	// 构造请求头
	reqHeader := http.Header{}
	if authKey != "" {
		reqHeader.Set("Authorization", "Bearer "+authKey)
	}

	// 尝试 WebSocket 握手
	start := time.Now()
	conn, resp, dialErr := dialer.Dial(wsURL, reqHeader)
	latency := time.Since(start)

	// gorilla/websocket 在握手失败时也可能返回非 nil resp，需关闭其 Body
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}

	if dialErr != nil {
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		slog.Info("admin: WebSocket 测试失败", "upstream_id", id, "error", dialErr)
		result := map[string]interface{}{
			"websocket_supported": false,
			"message":             fmt.Sprintf("WebSocket 连接失败: %v", dialErr),
			"latency_ms":          latency.Milliseconds(),
		}
		if statusCode > 0 {
			result["status_code"] = statusCode
		}
		jsonOK(w, result)
		return
	}

	// 连接成功 → 发送关闭帧后断开
	_ = conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	conn.Close()

	slog.Info("admin: WebSocket 测试成功", "upstream_id", id, "url", wsURL, "latency_ms", latency.Milliseconds())

	// 自动启用 websocket_enabled（仅更新该列，避免覆盖并发修改的其他字段）
	if err := h.store.SetWebSocketEnabled(id, true); err != nil {
		slog.Error("admin: 自动启用 websocket_enabled 失败", "error", err)
	} else {
		go func() { defer func() { recover() }(); h.prober.ProbeNow() }()
	}

	jsonOK(w, map[string]interface{}{
		"websocket_supported": true,
		"message":             "WebSocket 连接成功，已自动启用 WebSocket 代理",
		"latency_ms":          latency.Milliseconds(),
	})
}

// checkUpstreamQuota 通过 new-api 的 /api/usage/token 接口查询上游 Key 的剩余额度。
// 仅解析 new-api 风格的响应（code=true, data.object="token_usage"），
// 非 new-api 格式时返回截断的原始内容供管理员在 DevTools 中查看。
func (h *AdminHandler) checkUpstreamQuota(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 解析可选的 CF 绕过参数
	var cfOpts struct {
		CFClearance string `json:"cf_clearance"`
		CFUserAgent string `json:"cf_user_agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&cfOpts); err != nil && err.Error() != "EOF" {
		jsonError(w, http.StatusBadRequest, "invalid CF params JSON")
		return
	}

	upstream, err := h.store.GetUpstream(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("upstream %d not found", id))
		return
	}

	// 构造带代理的 HTTP client，复用 testUpstreamProxy 的安全策略
	transport, err := proxy.BuildTransport(upstream.ProxyURL)
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("invalid proxy config: %v", err),
		})
		return
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		// 允许同域重定向（如 /api/usage/token → /api/usage/token/），
		// 但跨域时阻止，防止 Authorization 头泄露到意外域名
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("cross-host redirect blocked: %s → %s", via[0].URL.Host, req.URL.Host)
			}
			return nil
		},
	}

	quotaURL := strings.TrimRight(upstream.BaseURL, "/") + "/api/usage/token"
	req, err := http.NewRequestWithContext(r.Context(), "GET", quotaURL, nil)
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("构造请求失败: %v", err),
		})
		return
	}
	var firstKey string
	if len(upstream.APIKeys) > 0 {
		firstKey = upstream.APIKeys[0]
	}
	req.Header.Set("Authorization", "Bearer "+firstKey)
	applyCFHeaders(req, cfOpts.CFClearance, cfOpts.CFUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("请求失败: %v", err),
		})
		return
	}
	defer resp.Body.Close()

	// 限制读取 64KB，防止大响应体占满内存（同时避免截断合法 JSON）
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("读取响应失败: %v", err),
		})
		return
	}

	// 非 2xx 状态码直接报错
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		jsonOK(w, map[string]interface{}{
			"success":        false,
			"message":        fmt.Sprintf("上游返回 HTTP %d", resp.StatusCode),
			"origin_content": string(body),
		})
		return
	}

	// Content-Type 非 JSON 时直接走"非 new-api"分支（大小写不敏感）
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(strings.ToLower(ct), "json") {
		jsonOK(w, map[string]interface{}{
			"success":        false,
			"message":        "error",
			"origin_content": string(body),
		})
		return
	}

	// 尝试解析 new-api 风格的响应
	var apiResp struct {
		Code    interface{} `json:"code"`
		Message string      `json:"message"`
		Data    struct {
			Object             string          `json:"object"`
			Name               string          `json:"name"`
			TotalAvailable     int64           `json:"total_available"`
			TotalGranted       int64           `json:"total_granted"`
			TotalUsed          int64           `json:"total_used"`
			UnlimitedQuota     bool            `json:"unlimited_quota"`
			ExpiresAt          int64           `json:"expires_at"`
			ModelLimitsEnabled bool            `json:"model_limits_enabled"`
			ModelLimits        map[string]bool `json:"model_limits"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		jsonOK(w, map[string]interface{}{
			"success":        false,
			"message":        "error",
			"origin_content": string(body),
		})
		return
	}

	// 校验是否为 new-api 风格：data.object 必须是 "token_usage"
	if apiResp.Data.Object != "token_usage" {
		jsonOK(w, map[string]interface{}{
			"success":        false,
			"message":        "error",
			"origin_content": string(body),
		})
		return
	}

	// 处理 code=false 的情况（new-api 返回错误）
	codeOK := false
	switch v := apiResp.Code.(type) {
	case bool:
		codeOK = v
	case float64:
		codeOK = v != 0
	}
	if !codeOK {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("上游返回错误: %s", apiResp.Message),
		})
		return
	}

	// 成功：返回解析后的额度信息
	jsonOK(w, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"name":                 apiResp.Data.Name,
			"total_available":      apiResp.Data.TotalAvailable,
			"total_granted":        apiResp.Data.TotalGranted,
			"total_used":           apiResp.Data.TotalUsed,
			"unlimited_quota":      apiResp.Data.UnlimitedQuota,
			"expires_at":           apiResp.Data.ExpiresAt,
			"model_limits_enabled": apiResp.Data.ModelLimitsEnabled,
			"model_limits":         apiResp.Data.ModelLimits,
		},
	})
}
