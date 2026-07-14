package proxy

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	xproxy "golang.org/x/net/proxy"
)

// wsUpgrader 用于将下游 HTTP 请求升级为 WebSocket 连接。
// CheckOrigin 无条件放行——作为透明代理，来源校验由上游负责。
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// IsWebSocketUpgrade 检查请求是否为 WebSocket 升级请求。
func IsWebSocketUpgrade(r *http.Request) bool {
	return websocket.IsWebSocketUpgrade(r)
}

// WebSocketProxy 将下游 WebSocket 连接透明代理到上游。
// upstreamURL: 完整的上游 WebSocket URL（如 wss://api.openai.com/v1/realtime?model=xxx）
// upstreamHeaders: 包含重写后的鉴权头
// proxyURL: 上游代理地址（空字符串表示直连）
//
// 返回值：仅在初始连接建立阶段失败时返回错误；
// 双向转发启动后的错误只记录日志，函数会阻塞直到连接关闭。
func WebSocketProxy(w http.ResponseWriter, r *http.Request, upstreamURL string, upstreamHeaders http.Header, proxyURL string) error {
	// —— 1. 构建上游拨号器 ——
	dialer, err := buildWSDialer(proxyURL)
	if err != nil {
		slog.Error("构建 WebSocket 拨号器失败", "proxy", proxyURL, "error", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return err
	}

	// —— 2. 拨号上游 ——
	slog.Info("正在建立上游 WebSocket 连接", "upstream", upstreamURL)
	upstreamConn, resp, err := dialer.Dial(upstreamURL, upstreamHeaders)
	if err != nil {
		slog.Error("拨号上游 WebSocket 失败", "upstream", upstreamURL, "error", err)
		if resp != nil {
			// 尝试将上游的 HTTP 错误状态码回写给客户端
			http.Error(w, "upstream websocket dial failed", resp.StatusCode)
			resp.Body.Close()
		} else {
			http.Error(w, "bad gateway", http.StatusBadGateway)
		}
		return err
	}
	defer upstreamConn.Close()
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	// —— 3. 升级下游客户端连接 ——
	clientConn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("升级下游 WebSocket 失败", "error", err)
		// Upgrade 失败时 gorilla 已向客户端写入了 HTTP 错误，无需再写
		return err
	}
	defer clientConn.Close()

	slog.Info("WebSocket 双向代理已建立",
		"upstream", upstreamURL,
		"client", r.RemoteAddr,
	)

	// —— 4. 启动双向转发 ——
	done := make(chan struct{}, 2)
	go pumpClientToUpstream(clientConn, upstreamConn, done)
	go pumpUpstreamToClient(upstreamConn, clientConn, done)

	// —— 5. 等待任一方向结束，清理并返回 ——
	<-done

	slog.Info("WebSocket 连接已关闭",
		"upstream", upstreamURL,
		"client", r.RemoteAddr,
	)
	return nil
}

// buildWSDialer 根据代理地址构建 WebSocket 拨号器。
// 空字符串表示直连；支持 http/https 和 socks5 代理。
func buildWSDialer(proxyURL string) (*websocket.Dialer, error) {
	if proxyURL == "" {
		return websocket.DefaultDialer, nil
	}

	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}

	switch parsed.Scheme {
	case "http", "https":
		return &websocket.Dialer{
			Proxy:            http.ProxyURL(parsed),
			HandshakeTimeout: 45 * time.Second,
		}, nil

	case "socks5":
		socks5Dialer, err := xproxy.FromURL(parsed, xproxy.Direct)
		if err != nil {
			return nil, err
		}
		// 优先使用 ContextDialer 接口
		if cd, ok := socks5Dialer.(xproxy.ContextDialer); ok {
			return &websocket.Dialer{
				NetDialContext:   cd.DialContext,
				HandshakeTimeout: 45 * time.Second,
			}, nil
		}
		// 回退到不带 context 的 Dial
		return &websocket.Dialer{
			NetDial:          socks5Dialer.Dial,
			HandshakeTimeout: 45 * time.Second,
		}, nil

	default:
		return nil, errors.New("不支持的代理协议: " + parsed.Scheme)
	}
}

// pumpClientToUpstream 从客户端读取消息并转发到上游。
// 当读取或写入失败时向 done 通道发送信号，并向对端发送关闭帧。
func pumpClientToUpstream(client, upstream *websocket.Conn, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()

	for {
		msgType, msg, err := client.ReadMessage()
		if err != nil {
			// 收到正常关闭帧或连接已断开
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				slog.Debug("客户端发送关闭帧", "error", err)
			} else if !isNetClosedError(err) {
				slog.Warn("读取客户端消息失败", "error", err)
			}
			// 将关闭帧传播到上游
			code, text := extractCloseCodeText(err)
			writeClose(upstream, code, text)
			return
		}

		if err := upstream.WriteMessage(msgType, msg); err != nil {
			slog.Warn("写入上游消息失败", "error", err)
			writeClose(client, websocket.CloseInternalServerErr, "upstream write error")
			return
		}
	}
}

// pumpUpstreamToClient 从上游读取消息并转发到客户端。
// 当读取或写入失败时向 done 通道发送信号，并向对端发送关闭帧。
func pumpUpstreamToClient(upstream, client *websocket.Conn, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()

	for {
		msgType, msg, err := upstream.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				slog.Debug("上游发送关闭帧", "error", err)
			} else if !isNetClosedError(err) {
				slog.Warn("读取上游消息失败", "error", err)
			}
			// 将关闭帧传播到客户端
			code, text := extractCloseCodeText(err)
			writeClose(client, code, text)
			return
		}

		if err := client.WriteMessage(msgType, msg); err != nil {
			slog.Warn("写入客户端消息失败", "error", err)
			writeClose(upstream, websocket.CloseInternalServerErr, "client write error")
			return
		}
	}
}

// extractCloseCodeText 从 WebSocket 错误中提取关闭码与原因文本。
// 如果无法解析，返回 CloseNormalClosure 和空字符串。
func extractCloseCodeText(err error) (int, string) {
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		return closeErr.Code, closeErr.Text
	}
	return websocket.CloseNormalClosure, ""
}

// writeClose 向连接写入关闭帧。忽略写入错误——对端可能已断开。
func writeClose(conn *websocket.Conn, code int, text string) {
	msg := websocket.FormatCloseMessage(code, text)
	deadline := time.Now().Add(5 * time.Second)
	_ = conn.WriteControl(websocket.CloseMessage, msg, deadline)
}

// isNetClosedError 判断错误是否为"连接已关闭"类型，
// 用于避免对正常断连打出不必要的警告日志。
func isNetClosedError(err error) bool {
	if err == nil {
		return false
	}
	// io.EOF 或 net.ErrClosed
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	// 捕获 "use of closed network connection" 等无法 unwrap 的情况
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	return false
}
