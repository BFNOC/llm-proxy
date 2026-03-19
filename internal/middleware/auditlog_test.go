package middleware

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var auditTestKey = []byte("01234567890123456789012345678901")

func newAuditTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "audit_test.db")
	s, err := store.NewStore(dbPath, auditTestKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// ---------------------------------------------------------------------------
// Unit tests — channel mechanics only (no real run/store)
// ---------------------------------------------------------------------------

func newChannelOnlyLogger(bufSize int) *AuditLogger {
	return &AuditLogger{
		ch:            make(chan store.RequestLog, bufSize),
		batchSize:     1000,
		flushInterval: time.Hour,
		stopCh:        make(chan struct{}),
		done:          make(chan struct{}),
	}
}

// TestAuditLogger_StopLog_NoPanic verifies that calling Log() concurrently
// with Stop() never panics (the stop-channel pattern eliminates the race).
func TestAuditLogger_StopLog_NoPanic(t *testing.T) {
	al := newChannelOnlyLogger(100)
	go func() {
		defer close(al.done)
		for {
			select {
			case <-al.ch:
			case <-al.stopCh:
				for {
					select {
					case <-al.ch:
					default:
						return
					}
				}
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			al.Log(store.RequestLog{ClientIP: "1.2.3.4"})
		}()
	}
	al.Stop()
	wg.Wait()
}

// TestAuditLogger_StopIdempotent verifies calling Stop() multiple times
// does not panic (sync.Once).
func TestAuditLogger_StopIdempotent(t *testing.T) {
	al := newChannelOnlyLogger(10)
	go func() {
		defer close(al.done)
		<-al.stopCh
	}()
	al.Stop()
	al.Stop()
	al.Stop()
}

// TestAuditLogger_LogAfterStop verifies no panic when logging after stop.
func TestAuditLogger_LogAfterStop(t *testing.T) {
	al := newChannelOnlyLogger(10)
	go func() {
		defer close(al.done)
		<-al.stopCh
	}()
	al.Stop()

	for i := 0; i < 100; i++ {
		al.Log(store.RequestLog{ClientIP: "1.2.3.4"})
	}
	assert.Greater(t, al.DroppedCount(), int64(0), "some logs should be dropped after stop")
}

// TestAuditLogger_DroppedCount verifies the counter when the channel is full.
func TestAuditLogger_DroppedCount(t *testing.T) {
	al := newChannelOnlyLogger(2)
	go func() {
		defer close(al.done)
		<-al.stopCh
	}()
	defer al.Stop()

	al.Log(store.RequestLog{ClientIP: "1.1.1.1"})
	al.Log(store.RequestLog{ClientIP: "2.2.2.2"})
	time.Sleep(10 * time.Millisecond)
	al.Log(store.RequestLog{ClientIP: "3.3.3.3"})

	assert.GreaterOrEqual(t, al.DroppedCount(), int64(1), "should have at least 1 dropped")
}

// ---------------------------------------------------------------------------
// Integration tests — real run() + real SQLite store
// ---------------------------------------------------------------------------

// TestAuditLogger_Integration_BatchFlush verifies that the real run() goroutine
// flushes a batch to the database when batchSize is reached.
func TestAuditLogger_Integration_BatchFlush(t *testing.T) {
	s := newAuditTestStore(t)
	// Need a downstream key for the foreign key.
	_, dk, err := s.CreateKey("batch-test", 0)
	require.NoError(t, err)

	// batchSize=3, huge flushInterval so only batch-size triggers flush.
	al := NewAuditLogger(s, nil, 100, 3, time.Hour)
	defer al.Stop()

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		al.Log(store.RequestLog{
			DownstreamKeyID: dk.ID,
			ClientIP:        "10.0.0.1",
			ProviderStyle:   "openai",
			Path:            "/v1/chat/completions",
			StatusCode:      200,
			LatencyMs:       int64(i * 10),
			CreatedAt:       now,
		})
	}

	// Wait for batch flush to complete.
	time.Sleep(100 * time.Millisecond)

	logs, err := s.QueryLogs(dk.ID, now.Add(-time.Minute), now.Add(time.Minute), 100)
	require.NoError(t, err)
	assert.Len(t, logs, 3, "all 3 logs should be flushed after batch is full")
}

// TestAuditLogger_Integration_TickerFlush verifies that the ticker triggers
// a flush even when the batch is not full.
func TestAuditLogger_Integration_TickerFlush(t *testing.T) {
	s := newAuditTestStore(t)
	_, dk, err := s.CreateKey("ticker-test", 0)
	require.NoError(t, err)

	// batchSize=100 (won't trigger), flushInterval=50ms (will trigger).
	al := NewAuditLogger(s, nil, 100, 100, 50*time.Millisecond)
	defer al.Stop()

	now := time.Now().UTC()
	al.Log(store.RequestLog{
		DownstreamKeyID: dk.ID,
		ClientIP:        "10.0.0.2",
		ProviderStyle:   "openai",
		Path:            "/v1/chat/completions",
		StatusCode:      200,
		LatencyMs:       42,
		CreatedAt:       now,
	})

	// Wait for ticker to fire and flush.
	time.Sleep(200 * time.Millisecond)

	logs, err := s.QueryLogs(dk.ID, now.Add(-time.Minute), now.Add(time.Minute), 100)
	require.NoError(t, err)
	assert.Len(t, logs, 1, "log should be flushed by ticker")
}

// TestAuditLogger_Integration_StopDrains verifies that Stop() drains all
// remaining logs to the database before returning.
func TestAuditLogger_Integration_StopDrains(t *testing.T) {
	s := newAuditTestStore(t)
	_, dk, err := s.CreateKey("drain-test", 0)
	require.NoError(t, err)

	// Large batch + huge interval — only Stop drain should flush.
	al := NewAuditLogger(s, nil, 100, 1000, time.Hour)

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		al.Log(store.RequestLog{
			DownstreamKeyID: dk.ID,
			ClientIP:        "10.0.0.3",
			ProviderStyle:   "openai",
			Path:            "/v1/completions",
			StatusCode:      200,
			LatencyMs:       int64(i),
			CreatedAt:       now,
		})
	}

	// Stop should drain all 5 logs.
	al.Stop()

	logs, err := s.QueryLogs(dk.ID, now.Add(-time.Minute), now.Add(time.Minute), 100)
	require.NoError(t, err)
	assert.Len(t, logs, 5, "all logs should be drained on Stop()")
}

// TestAuditLogger_Integration_IPRegionPersisted verifies that IPRegion set
// during enrichment is actually written to and read from the database.
func TestAuditLogger_Integration_IPRegionPersisted(t *testing.T) {
	s := newAuditTestStore(t)
	_, dk, err := s.CreateKey("region-test", 0)
	require.NoError(t, err)

	// No GeoIP — we set IPRegion manually to test the store round-trip.
	al := NewAuditLogger(s, nil, 100, 1, 50*time.Millisecond)
	defer al.Stop()

	now := time.Now().UTC()
	// Simulate a log that already has IPRegion set (as if enriched).
	al.Log(store.RequestLog{
		DownstreamKeyID: dk.ID,
		ClientIP:        "114.114.114.114",
		IPRegion:        "中国|江苏省|南京市|CN",
		ProviderStyle:   "openai",
		Path:            "/v1/chat/completions",
		StatusCode:      200,
		LatencyMs:       50,
		CreatedAt:       now,
	})

	// Wait for flush.
	time.Sleep(200 * time.Millisecond)

	logs, err := s.QueryLogs(dk.ID, now.Add(-time.Minute), now.Add(time.Minute), 100)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "中国|江苏省|南京市|CN", logs[0].IPRegion, "IPRegion should round-trip through the DB")
	assert.Equal(t, "114.114.114.114", logs[0].ClientIP)
}

// TestAuditLogger_Integration_InsertErrorNoFatal verifies that a store error
// during flush does not crash the run() goroutine — it logs an error and
// continues processing.
func TestAuditLogger_Integration_InsertErrorNoFatal(t *testing.T) {
	s := newAuditTestStore(t)

	al := NewAuditLogger(s, nil, 100, 1, 50*time.Millisecond)

	// Close the store to simulate a DB error during flush.
	_ = s.Close()

	now := time.Now().UTC()
	al.Log(store.RequestLog{
		DownstreamKeyID: 1,
		ClientIP:        "10.0.0.4",
		ProviderStyle:   "openai",
		Path:            "/v1/chat",
		StatusCode:      200,
		LatencyMs:       10,
		CreatedAt:       now,
	})

	// Wait for flush attempt (should error but not panic).
	time.Sleep(200 * time.Millisecond)

	// Stop should complete without hanging.
	al.Stop()
}
