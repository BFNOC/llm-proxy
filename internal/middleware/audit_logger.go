package middleware

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Instawork/llm-proxy/internal/geoip"
	"github.com/Instawork/llm-proxy/internal/store"
)

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
