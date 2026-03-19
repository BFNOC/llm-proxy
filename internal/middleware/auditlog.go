package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Instawork/llm-proxy/internal/geoip"
	"github.com/Instawork/llm-proxy/internal/store"
)

// AuditLogger 异步收集请求日志并批量写入 SQLite。
type AuditLogger struct {
	ch            chan store.RequestLog
	store         *store.Store
	geoIP         *geoip.GeoIP // 可为 nil（优雅降级）
	batchSize     int
	flushInterval time.Duration
	droppedCount  int64
	stopCh        chan struct{} // 由 Stop() 关闭以通知停机
	done          chan struct{} // 由 run() 在排空完成后关闭
	stopOnce      sync.Once
}

// NewAuditLogger 创建并启动审计日志记录器。
// bufferSize 为通道缓冲大小，batchSize 为批量写入阈值，flushInterval 为定时刷新间隔。
func NewAuditLogger(s *store.Store, geo *geoip.GeoIP, bufferSize, batchSize int, flushInterval time.Duration) *AuditLogger {
	al := &AuditLogger{
		ch:            make(chan store.RequestLog, bufferSize),
		store:         s,
		geoIP:         geo,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		stopCh:        make(chan struct{}),
		done:          make(chan struct{}),
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

// Log 将请求日志入队。如果已停机或通道已满，日志将被丢弃并计数。
// 使用 stopCh 模式避免 close(ch) 与 send 的数据竞争，可安全地与 Stop() 并发调用。
func (al *AuditLogger) Log(log store.RequestLog) {
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

// responseStatusCapture 包装 ResponseWriter，用于捕获状态码和内部头信息
// （如 X-Upstream-Name），在 WriteHeader 发送给客户端前拦截。
type responseStatusCapture struct {
	http.ResponseWriter
	statusCode   int
	upstreamName string // 从 X-Upstream-Name 头捕获
}

func (r *responseStatusCapture) WriteHeader(code int) {
	r.statusCode = code
	// 在响应发送给客户端前捕获并移除内部头。
	r.upstreamName = r.Header().Get("X-Upstream-Name")
	r.Header().Del("X-Upstream-Name")
	r.ResponseWriter.WriteHeader(code)
}

// AuditLogMiddleware 通过 AuditLogger 异步记录请求元数据。
func AuditLogMiddleware(logger *AuditLogger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			capture := &responseStatusCapture{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(capture, r)

			latency := time.Since(start).Milliseconds()

			style := StyleFromContext(r.Context())
			keyID := DownstreamKeyIDFromContext(r.Context())
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

			logger.Log(store.RequestLog{
				DownstreamKeyID: keyID,
				UpstreamName:    upstreamName,
				ClientIP:        clientIP,
				ProviderStyle:   string(style),
				Path:            r.URL.Path,
				StatusCode:      capture.statusCode,
				LatencyMs:       latency,
				CreatedAt:       time.Now().UTC(),
			})
		})
	}
}
