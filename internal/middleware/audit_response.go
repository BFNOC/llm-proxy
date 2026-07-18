package middleware

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strconv"
)

// responseStatusCapture 包装 ResponseWriter，用于捕获状态码、内部头信息
// （如 X-Upstream-Name, X-API-Key-Index）以及响应体大小，在 WriteHeader 发送给客户端前拦截。
type responseStatusCapture struct {
	http.ResponseWriter
	statusCode     int
	upstreamName   string // 从 X-Upstream-Name 头捕获
	upstreamKeyIdx int    // 从 X-API-Key-Index 头捕获
	model          string // 从 X-Model 头捕获
	usedProxy      string // 从 X-Used-Proxy 头捕获
	responseSize   int64  // 累计写入的响应体字节数
	wroteHeader    bool
	captureFull    bool
	responseBody   limitedBodyCapture
	responseHeader http.Header
	hijacked       bool
}

func (r *responseStatusCapture) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.statusCode = code
	// 在响应发送给客户端前捕获并移除内部头。
	r.upstreamName = r.Header().Get("X-Upstream-Name")
	r.Header().Del("X-Upstream-Name")
	r.upstreamKeyIdx = -1
	if v := r.Header().Get("X-API-Key-Index"); v != "" {
		if idx, err := strconv.Atoi(v); err == nil {
			r.upstreamKeyIdx = idx
		}
	}
	r.Header().Del("X-API-Key-Index")
	r.model = r.Header().Get("X-Model")
	r.Header().Del("X-Model")
	r.usedProxy = r.Header().Get("X-Used-Proxy")
	r.Header().Del("X-Used-Proxy")
	if r.captureFull {
		r.responseHeader = sanitizeResponseHeaders(r.Header())
	}
	r.ResponseWriter.WriteHeader(code)
}

// Write 拦截响应体写入以累计响应大小。
func (r *responseStatusCapture) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.responseSize += int64(n)
	if r.captureFull && n > 0 {
		r.responseBody.append(b[:n])
	}
	return n, err
}

// Flush 透传底层 ResponseWriter 的流式刷新能力。
// 审计中间件包在 StreamingMiddleware 外层，若这里不暴露 http.Flusher，
// SSE 响应会被后续链路误判为不可刷新，最终按大块缓冲返回。
func (r *responseStatusCapture) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack 透传底层连接劫持能力，保持 ResponseWriter 包装器透明。
// 当前 LLM 流式接口使用 SSE，不依赖 Hijack；这里用于避免未来代理特殊连接时被审计包装器截断能力。
func (r *responseStatusCapture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := r.ResponseWriter.(http.Hijacker); ok {
		conn, rw, err := hijacker.Hijack()
		if err == nil {
			r.hijacked = true
		}
		return conn, rw, err
	}
	return nil, nil, errors.ErrUnsupported
}
