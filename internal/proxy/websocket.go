package proxy

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
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

// maxWSMessageSize 限制单条 WebSocket 消息的最大大小（16 MB）。
const maxWSMessageSize = 16 << 20

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

	// 转发客户端请求的子协议（如 OpenAI Realtime 的 "realtime"）
	dialer.Subprotocols = websocket.Subprotocols(r)

	// —— 2. 拨号上游（需要先获取协商的子协议才能正确升级客户端） ——
	slog.Info("正在建立上游 WebSocket 连接", "upstream", upstreamURL)
	upstreamConn, resp, err := dialer.Dial(upstreamURL, upstreamHeaders)
	if err != nil {
		slog.Error("拨号上游 WebSocket 失败", "upstream", upstreamURL, "error", err)
		if resp != nil {
			code := resp.StatusCode
			if code < 400 || code > 599 {
				code = http.StatusBadGateway
			}
			http.Error(w, "upstream websocket dial failed", code)
			resp.Body.Close()
		} else {
			http.Error(w, "bad gateway", http.StatusBadGateway)
		}
		return err
	}
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	// —— 3. 升级下游客户端连接，传递上游协商的子协议 ——
	var upgradeRespHeader http.Header
	if proto := upstreamConn.Subprotocol(); proto != "" {
		upgradeRespHeader = http.Header{}
		upgradeRespHeader.Set("Sec-WebSocket-Protocol", proto)
	}

	clientConn, err := wsUpgrader.Upgrade(w, r, upgradeRespHeader)
	if err != nil {
		slog.Error("升级下游 WebSocket 失败", "error", err)
		upstreamConn.Close()
		return err
	}

	// 设置消息大小限制，防止内存耗尽
	clientConn.SetReadLimit(maxWSMessageSize)
	upstreamConn.SetReadLimit(maxWSMessageSize)

	slog.Info("WebSocket 双向代理已建立",
		"upstream", upstreamURL,
		"client", r.RemoteAddr,
	)

	// —— 4. 启动双向转发 ——
	// 每个连接用一个 mutex 保护写操作，避免 gorilla/websocket 的并发写竞争。
	var clientMu, upstreamMu sync.Mutex
	done := make(chan struct{}, 2)

	go pumpMessages(clientConn, upstreamConn, &upstreamMu, &clientMu, done, "client", "upstream")
	go pumpMessages(upstreamConn, clientConn, &clientMu, &upstreamMu, done, "upstream", "client")

	// —— 5. 等待任一方向结束，关闭两端连接，再等另一方向退出 ——
	<-done
	clientConn.Close()
	upstreamConn.Close()
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
		return &websocket.Dialer{
			HandshakeTimeout: 45 * time.Second,
		}, nil
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
		if cd, ok := socks5Dialer.(xproxy.ContextDialer); ok {
			return &websocket.Dialer{
				NetDialContext:   cd.DialContext,
				HandshakeTimeout: 45 * time.Second,
			}, nil
		}
		return &websocket.Dialer{
			NetDial:          socks5Dialer.Dial,
			HandshakeTimeout: 45 * time.Second,
		}, nil

	default:
		return nil, errors.New("不支持的代理协议: " + parsed.Scheme)
	}
}

// pumpMessages 从 src 读取消息并通过 dstMu 保护写入 dst。
// 读取或写入失败时向对端发送关闭帧（通过对应 mutex 保护），然后通知 done 通道。
func pumpMessages(src, dst *websocket.Conn, dstMu, srcMu *sync.Mutex, done chan<- struct{}, srcName, dstName string) {
	defer func() { done <- struct{}{} }()

	for {
		msgType, msg, err := src.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				slog.Debug(srcName+"发送关闭帧", "error", err)
			} else if !isNetClosedError(err) {
				slog.Warn("读取"+srcName+"消息失败", "error", err)
			}
			code, text := extractCloseCodeText(err)
			dstMu.Lock()
			writeClose(dst, code, text)
			dstMu.Unlock()
			return
		}

		dstMu.Lock()
		writeErr := dst.WriteMessage(msgType, msg)
		dstMu.Unlock()
		if writeErr != nil {
			slog.Warn("写入"+dstName+"消息失败", "error", writeErr)
			srcMu.Lock()
			writeClose(src, websocket.CloseInternalServerErr, dstName+" write error")
			srcMu.Unlock()
			return
		}
	}
}

// extractCloseCodeText 从 WebSocket 错误中提取关闭码与原因文本。
// 如果无法解析，返回 CloseInternalServerErr 表示异常断开。
func extractCloseCodeText(err error) (int, string) {
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		return closeErr.Code, closeErr.Text
	}
	return websocket.CloseInternalServerErr, ""
}

// writeClose 向连接写入关闭帧。忽略写入错误——对端可能已断开。
// 调用方必须持有对应连接的写锁。
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
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return errors.Is(opErr.Err, net.ErrClosed)
	}
	return false
}
