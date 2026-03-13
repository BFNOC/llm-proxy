package middleware

import (
	"log/slog"
	"net/http"
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

// responseStatusCapture wraps ResponseWriter to capture the status code.
type responseStatusCapture struct {
	http.ResponseWriter
	statusCode int
}

func (r *responseStatusCapture) WriteHeader(code int) {
	r.statusCode = code
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

			logger.Log(store.RequestLog{
				DownstreamKeyID: keyID,
				ProviderStyle:   string(style),
				Path:            r.URL.Path,
				StatusCode:      capture.statusCode,
				LatencyMs:       latency,
				CreatedAt:       time.Now().UTC(),
			})
		})
	}
}
