package middleware

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Instawork/llm-proxy/internal/geoip"
	"github.com/Instawork/llm-proxy/internal/store"
)

const (
	maxFullRecordBodyBytes     = 32 << 20
	maxFullRecordRetainedBytes = 64 << 20
	fullRecordEnqueueTimeout   = time.Second
)

// fullRecordMemoryBudget 限制尚未落库的完整正文总量，避免大请求在异步队列中放大内存占用。
type fullRecordMemoryBudget struct {
	used  int64
	limit int64
}

func (b *fullRecordMemoryBudget) reserve(size int) int {
	if b == nil || size <= 0 {
		return size
	}
	for {
		used := atomic.LoadInt64(&b.used)
		remaining := b.limit - used
		if remaining <= 0 {
			return 0
		}
		granted := int64(size)
		if granted > remaining {
			granted = remaining
		}
		if atomic.CompareAndSwapInt64(&b.used, used, used+granted) {
			return int(granted)
		}
	}
}

func (b *fullRecordMemoryBudget) release(size int64) {
	if b == nil || size <= 0 {
		return
	}
	for {
		used := atomic.LoadInt64(&b.used)
		if used <= 0 {
			return
		}
		released := size
		if released > used {
			released = used
		}
		if atomic.CompareAndSwapInt64(&b.used, used, used-released) {
			return
		}
	}
}

type limitedBodyCapture struct {
	data      []byte
	limit     int
	truncated bool
	budget    *fullRecordMemoryBudget
	reserved  int64
}

func (c *limitedBodyCapture) append(data []byte) {
	remaining := c.limit - len(c.data)
	if remaining <= 0 {
		if len(data) > 0 {
			c.truncated = true
		}
		return
	}
	wanted := len(data)
	if wanted > remaining {
		wanted = remaining
		c.truncated = true
	}
	granted := c.budget.reserve(wanted)
	if granted < wanted {
		c.truncated = true
	}
	if granted > 0 {
		c.data = append(c.data, data[:granted]...)
		c.reserved += int64(granted)
	}
}

func (c *limitedBodyCapture) takeString() (string, int64) {
	value := string(c.data)
	reserved := c.reserved
	c.data = nil
	c.reserved = 0
	return value, reserved
}

func (c *limitedBodyCapture) releaseReserved() {
	if c == nil {
		return
	}
	c.budget.release(c.reserved)
	c.data = nil
	c.reserved = 0
}

type bodyCaptureReadCloser struct {
	io.ReadCloser
	capture *limitedBodyCapture
	read    bool
}

func (r *bodyCaptureReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.read = true
		r.capture.append(p[:n])
	}
	return n, err
}

// AuditLogger 异步收集请求日志并批量写入 SQLite。
type AuditLogger struct {
	ch             chan store.RequestLog
	store          *store.Store
	geoIP          *geoip.GeoIP // 可为 nil（优雅降级）
	batchSize      int
	flushInterval  time.Duration
	fullRecordWait time.Duration
	droppedCount   int64
	fullRecordMem  *fullRecordMemoryBudget
	stopCh         chan struct{} // 由 Stop() 关闭以通知停机
	done           chan struct{} // 由 run() 在排空完成后关闭
	stopOnce       sync.Once
}

// NewAuditLogger 创建并启动审计日志记录器。
// bufferSize 为通道缓冲大小，batchSize 为批量写入阈值，flushInterval 为定时刷新间隔。
func NewAuditLogger(s *store.Store, geo *geoip.GeoIP, bufferSize, batchSize int, flushInterval time.Duration) *AuditLogger {
	al := &AuditLogger{
		ch:             make(chan store.RequestLog, bufferSize),
		store:          s,
		geoIP:          geo,
		batchSize:      batchSize,
		flushInterval:  flushInterval,
		fullRecordWait: fullRecordEnqueueTimeout,
		fullRecordMem:  &fullRecordMemoryBudget{limit: maxFullRecordRetainedBytes},
		stopCh:         make(chan struct{}),
		done:           make(chan struct{}),
	}
	go al.run()
	return al
}

// run 是后台写入 goroutine，负责攒批、定时刷新和停机排空。
func (al *AuditLogger) run() {
	defer close(al.done)

	batch := make([]store.RequestLog, 0, al.batchSize)
	ticker := time.NewTicker(al.flushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		// 在写入 goroutine 中补充 IP 归属地（不在请求热路径中执行）。
		if al.geoIP != nil {
			for i := range batch {
				if batch[i].IPRegion == "" && batch[i].ClientIP != "" {
					batch[i].IPRegion = al.geoIP.Lookup(batch[i].ClientIP)
				}
			}
		}
		if err := al.store.InsertRequestLogBatch(batch); err != nil {
			slog.Error("audit: 批量写入失败", "error", err, "count", len(batch))
		}
		for i := range batch {
			al.fullRecordMem.release(batch[i].RetainedDetailBytes)
		}
		batch = batch[:0]
	}

	for {
		select {
		case log := <-al.ch:
			batch = append(batch, log)
			if len(batch) >= al.batchSize {
				flush()
			}
		case <-al.stopCh:
			// 停机时排空通道中剩余的日志。
			for {
				select {
				case log := <-al.ch:
					batch = append(batch, log)
				default:
					flush()
					return
				}
			}
		case <-ticker.C:
			flush()
		}
	}
}

// Log 将请求日志入队。完整记录先短暂回压，超时后丢弃，避免存储停滞造成请求 goroutine 无界堆积。
// 使用 stopCh 模式避免 close(ch) 与 send 的数据竞争，可安全地与 Stop() 并发调用。
func (al *AuditLogger) Log(log store.RequestLog) {
	if log.Detail != nil {
		select {
		case <-al.stopCh:
			al.fullRecordMem.release(log.RetainedDetailBytes)
			atomic.AddInt64(&al.droppedCount, 1)
			return
		case al.ch <- log:
			return
		default:
		}
		wait := al.fullRecordWait
		if wait <= 0 {
			wait = fullRecordEnqueueTimeout
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-al.stopCh:
			al.fullRecordMem.release(log.RetainedDetailBytes)
			atomic.AddInt64(&al.droppedCount, 1)
		case al.ch <- log:
		case <-timer.C:
			al.fullRecordMem.release(log.RetainedDetailBytes)
			atomic.AddInt64(&al.droppedCount, 1)
		}
		return
	}
	select {
	case <-al.stopCh:
		// 已停机，丢弃日志。
		atomic.AddInt64(&al.droppedCount, 1)
	case al.ch <- log:
		// 入队成功。
	default:
		// 通道已满，丢弃日志。
		atomic.AddInt64(&al.droppedCount, 1)
	}
}

// DroppedCount 返回因通道满或已停机而丢弃的日志条数。
func (al *AuditLogger) DroppedCount() int64 {
	return atomic.LoadInt64(&al.droppedCount)
}

// Stop 通知写入 goroutine 排空并退出，然后等待完成。
// 可多次调用（通过 sync.Once 保证幂等性）。
func (al *AuditLogger) Stop() {
	al.stopOnce.Do(func() {
		close(al.stopCh)
	})
	<-al.done
}

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

// AuditLogMiddleware 通过 AuditLogger 异步记录请求元数据，并按运行时策略附带完整记录。
func AuditLogMiddleware(logger *AuditLogger, policies ...*FullRecordingPolicy) func(http.Handler) http.Handler {
	var policy *FullRecordingPolicy
	if len(policies) > 0 {
		policy = policies[0]
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			keyID := DownstreamKeyIDFromContext(r.Context())
			captureFull := policy.ShouldRecord(keyID)
			isWebSocket := isWebSocketUpgrade(r)

			var requestCapture *bodyCaptureReadCloser
			requestHeadersJSON := "{}"
			if captureFull {
				requestHeadersJSON = marshalHeaders(sanitizeRequestHeaders(r.Header))
				if !isWebSocket && r.Body != nil && r.Body != http.NoBody {
					requestCapture = &bodyCaptureReadCloser{
						ReadCloser: r.Body,
						capture:    &limitedBodyCapture{limit: maxFullRecordBodyBytes, budget: logger.fullRecordMem},
					}
					r.Body = requestCapture
				}
			}

			// 请求体大小：优先使用 ContentLength，未知时记为 0。
			requestSize := r.ContentLength
			if requestSize < 0 {
				requestSize = 0
			}

			capture := &responseStatusCapture{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				captureFull:    captureFull && !isWebSocket,
				responseBody:   limitedBodyCapture{limit: maxFullRecordBodyBytes, budget: logger.fullRecordMem},
			}
			defer func() {
				if requestCapture != nil {
					requestCapture.capture.releaseReserved()
				}
				capture.responseBody.releaseReserved()
			}()
			next.ServeHTTP(capture, r)
			if !capture.wroteHeader && !capture.hijacked {
				capture.WriteHeader(http.StatusOK)
			}

			latency := time.Since(start).Milliseconds()

			style := StyleFromContext(r.Context())
			upstreamName := capture.upstreamName

			// 提取客户端 IP，优先级：CF-Connecting-IP > X-Real-IP > X-Forwarded-For > RemoteAddr。
			clientIP := r.Header.Get("CF-Connecting-IP")
			if clientIP == "" {
				clientIP = r.Header.Get("X-Real-IP")
			}
			if clientIP == "" {
				clientIP = r.Header.Get("X-Forwarded-For")
				if clientIP != "" {
					// X-Forwarded-For 可能包含多个 IP，取第一个。
					if idx := strings.Index(clientIP, ","); idx != -1 {
						clientIP = strings.TrimSpace(clientIP[:idx])
					}
				}
			}
			if clientIP == "" {
				clientIP = r.RemoteAddr
				// 从 RemoteAddr 中去除端口号（如 "1.2.3.4:12345" -> "1.2.3.4"）。
				if host, _, err := net.SplitHostPort(clientIP); err == nil {
					clientIP = host
				}
			}

			logEntry := store.RequestLog{
				DownstreamKeyID: keyID,
				UpstreamName:    upstreamName,
				UpstreamKeyIdx:  capture.upstreamKeyIdx,
				Model:           capture.model,
				UsedProxy:       capture.usedProxy,
				ClientIP:        clientIP,
				ProviderStyle:   string(style),
				Path:            r.URL.Path,
				StatusCode:      capture.statusCode,
				LatencyMs:       latency,
				RequestSize:     requestSize,
				ResponseSize:    capture.responseSize,
				CreatedAt:       time.Now().UTC(),
			}
			if captureFull {
				logEntry.Detail, logEntry.RetainedDetailBytes = buildRequestLogDetail(r, requestCapture, requestHeadersJSON, capture, isWebSocket, keyID, string(style))
			}
			logger.Log(logEntry)
		})
	}
}

func buildRequestLogDetail(r *http.Request, requestCapture *bodyCaptureReadCloser, requestHeadersJSON string, response *responseStatusCapture, isWebSocket bool, keyID int64, providerStyle string) (*store.RequestLogDetail, int64) {
	detail := &store.RequestLogDetail{
		Method:              r.Method,
		RawQuery:            sanitizeRawQuery(r.URL.RawQuery),
		RequestHeadersJSON:  requestHeadersJSON,
		ResponseHeadersJSON: marshalHeaders(response.responseHeader),
		CaptureStatus:       "captured",
	}
	if isWebSocket {
		detail.CaptureStatus = "websocket_handshake"
		session := extractSessionMetadata(r.Header, nil, nil, keyID, providerStyle, r.URL.Path)
		detail.SessionID = session.ID
		detail.SessionSource = session.Source
		return detail, 0
	}
	var retainedBytes int64
	if requestCapture != nil {
		detail.RequestBody, retainedBytes = requestCapture.capture.takeString()
		detail.RequestBodyTruncated = requestCapture.capture.truncated
		if !requestCapture.read && r.ContentLength != 0 {
			detail.CaptureStatus = "request_body_unread"
		}
	}
	var responseBytes int64
	detail.ResponseBody, responseBytes = response.responseBody.takeString()
	retainedBytes += responseBytes
	detail.ResponseBodyTruncated = response.responseBody.truncated
	session := extractSessionMetadata(r.Header, []byte(detail.RequestBody), []byte(detail.ResponseBody), keyID, providerStyle, r.URL.Path)
	detail.SessionID = session.ID
	detail.SessionSource = session.Source
	detail.SessionPreview = session.Preview
	detail.ResponseID = session.ResponseID
	detail.ParentResponseID = session.ParentResponseID
	if detail.RequestBodyTruncated || detail.ResponseBodyTruncated {
		detail.CaptureStatus = "truncated"
	}
	return detail, retainedBytes
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && headerContainsToken(r.Header, "Connection", "upgrade")
}

func headerContainsToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func sanitizeRequestHeaders(header http.Header) http.Header {
	return redactSensitiveHeaders(header)
}

func sanitizeResponseHeaders(header http.Header) http.Header {
	return redactSensitiveHeaders(header)
}

func redactSensitiveHeaders(header http.Header) http.Header {
	result := make(http.Header)
	for name, values := range header {
		canonicalName := http.CanonicalHeaderKey(name)
		if isSensitiveName(canonicalName) {
			result[canonicalName] = []string{"[REDACTED]"}
		} else {
			result[canonicalName] = append([]string(nil), values...)
		}
	}
	return result
}

func sanitizeRawQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "[REDACTED_INVALID_QUERY]"
	}
	for name := range values {
		if isSensitiveName(name) {
			values[name] = []string{"[REDACTED]"}
		}
	}
	return values.Encode()
}

func isSensitiveName(name string) bool {
	lowerName := strings.ToLower(strings.TrimSpace(name))
	compactName := strings.NewReplacer("-", "", "_", "", ".", "").Replace(lowerName)
	if lowerName == "cookie" || lowerName == "set-cookie" || lowerName == "auth" || compactName == "xauth" || strings.Contains(compactName, "authorization") {
		return true
	}
	for _, marker := range []string{"apikey", "accesstoken", "authtoken", "refreshtoken", "securitytoken", "clientsecret", "password", "passwd", "credential", "signature"} {
		if strings.Contains(compactName, marker) {
			return true
		}
	}
	return lowerName == "token" || lowerName == "secret" || lowerName == "key"
}

func marshalHeaders(header http.Header) string {
	if len(header) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(header)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}
