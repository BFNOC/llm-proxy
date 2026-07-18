package proxy

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// serveWebSocket 处理 WebSocket 升级请求：选择支持 WS 的上游，重写鉴权头，透明代理。
func (dp *DynamicProxy) serveWebSocket(w http.ResponseWriter, r *http.Request, upstreams []*ActiveUpstream) {
	// 筛选支持 WebSocket 的上游
	var wsUpstreams []*ActiveUpstream
	for _, u := range upstreams {
		if u.WebSocketEnabled {
			wsUpstreams = append(wsUpstreams, u)
		}
	}
	if len(wsUpstreams) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "no upstream supports websocket"}) //nolint:errcheck
		return
	}

	active := wsUpstreams[0]
	apiKey, keyIdx, _ := active.NextAPIKey()

	// 构造上游 WebSocket URL：将 http(s) 替换为 ws(s)，保留路径和查询参数
	scheme := "wss"
	if active.BaseURL.Scheme == "http" {
		scheme = "ws"
	}
	upstreamURL := scheme + "://" + active.BaseURL.Host + r.URL.Path
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	// 构造上游鉴权头
	upstreamHeaders := http.Header{}
	style := DetectProviderStyle(r)
	if apiKey != "" {
		if style == StyleAnthropic && active.AuthMode != "oauth" {
			upstreamHeaders.Set("x-api-key", apiKey)
		} else {
			upstreamHeaders.Set("Authorization", "Bearer "+apiKey)
		}
	}
	// 转发 OpenAI-Beta 等协议头
	for _, h := range []string{"OpenAI-Beta", "Anthropic-Version"} {
		if v := r.Header.Get(h); v != "" {
			upstreamHeaders.Set(h, v)
		}
	}

	slog.Info("websocket proxy started",
		"upstream", active.Name, "key_index", keyIdx,
		"url", upstreamURL)

	// 不在 w.Header() 上设置 X-Upstream-Name / X-API-Key-Index：
	// WS 升级会绕过审计中间件的 header 清理逻辑，设置后会泄漏给客户端。
	// 上游信息已通过 slog 记录，满足可观测性需求。

	if err := WebSocketProxy(w, r, upstreamURL, upstreamHeaders, active.ProxyURL); err != nil {
		slog.Error("websocket proxy failed", "error", err, "upstream", active.Name)
	}
}
