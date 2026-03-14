package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Instawork/llm-proxy/internal/store"
)

// AuditLogger collects request logs asynchronously and batches them to SQLite.
type AuditLogger struct {
	ch            chan store.RequestLog
	store         *store.Store
	batchSize     int
	flushInterval time.Duration
	droppedCount  int64
	done          chan struct{}
}

func NewAuditLogger(s *store.Store, bufferSize, batchSize int, flushInterval time.Duration) *AuditLogger {
	al := &AuditLogger{
		ch:            make(chan store.RequestLog, bufferSize),
		store:         s,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		done:          make(chan struct{}),
	}
	go al.run()
	return al
}

func (al *AuditLogger) run() {
	defer close(al.done)

	batch := make([]store.RequestLog, 0, al.batchSize)
	ticker := time.NewTicker(al.flushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := al.store.InsertRequestLogBatch(batch); err != nil {
			slog.Error("audit: batch insert failed", "error", err, "count", len(batch))
		}
		batch = batch[:0]
	}

	for {
		select {
		case log, ok := <-al.ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, log)
			if len(batch) >= al.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// Log enqueues a request log. If the channel is full the entry is dropped and
// the dropped counter is incremented.
func (al *AuditLogger) Log(log store.RequestLog) {
	select {
	case al.ch <- log:
	default:
		atomic.AddInt64(&al.droppedCount, 1)
	}
}

// DroppedCount returns the number of log entries dropped due to a full buffer.
func (al *AuditLogger) DroppedCount() int64 {
	return atomic.LoadInt64(&al.droppedCount)
}

// Stop closes the channel and waits for the writer goroutine to drain.
func (al *AuditLogger) Stop() {
	close(al.ch)
	<-al.done
}

// responseStatusCapture wraps ResponseWriter to capture the status code and
// intercept internal headers (like X-Upstream-Name) before they are sent to
// the client via WriteHeader.
type responseStatusCapture struct {
	http.ResponseWriter
	statusCode   int
	upstreamName string // captured from X-Upstream-Name header
}

func (r *responseStatusCapture) WriteHeader(code int) {
	r.statusCode = code
	// Capture and strip internal header BEFORE flushing to client.
	r.upstreamName = r.Header().Get("X-Upstream-Name")
	r.Header().Del("X-Upstream-Name")
	r.ResponseWriter.WriteHeader(code)
}

// AuditLogMiddleware records request metadata asynchronously via AuditLogger.
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

			// Extract client IP: CF-Connecting-IP > X-Real-IP > X-Forwarded-For > RemoteAddr.
			clientIP := r.Header.Get("CF-Connecting-IP")
			if clientIP == "" {
				clientIP = r.Header.Get("X-Real-IP")
			}
			if clientIP == "" {
				clientIP = r.Header.Get("X-Forwarded-For")
				if clientIP != "" {
					// X-Forwarded-For may contain multiple IPs; take the first.
					if idx := strings.Index(clientIP, ","); idx != -1 {
						clientIP = strings.TrimSpace(clientIP[:idx])
					}
				}
			}
			if clientIP == "" {
				clientIP = r.RemoteAddr
				// Strip port from RemoteAddr (e.g. "1.2.3.4:12345" -> "1.2.3.4").
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
