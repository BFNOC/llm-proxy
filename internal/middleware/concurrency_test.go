package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ConcurrencyLimiter 单元测试
// ---------------------------------------------------------------------------

// TestConcurrencyLimiter_AcquireRelease 验证基本的获取→计数+1→释放→计数归零流程。
func TestConcurrencyLimiter_AcquireRelease(t *testing.T) {
	cl := NewConcurrencyLimiter()

	ok := cl.Acquire(1, 5)
	require.True(t, ok, "首次 Acquire 应当成功")
	assert.Equal(t, int64(1), cl.GetCount(1), "Acquire 后计数应为 1")

	cl.Release(1)
	assert.Equal(t, int64(0), cl.GetCount(1), "Release 后计数应归零")
}

// TestConcurrencyLimiter_LimitEnforced 验证并发数达上限后 Acquire 返回 false。
func TestConcurrencyLimiter_LimitEnforced(t *testing.T) {
	cl := NewConcurrencyLimiter()
	limit := 3

	// 连续获取到上限
	for i := 0; i < limit; i++ {
		ok := cl.Acquire(1, limit)
		require.True(t, ok, "第 %d 次 Acquire 应当成功", i+1)
	}
	assert.Equal(t, int64(limit), cl.GetCount(1), "达到上限时计数应等于 limit")

	// 超限应当被拒绝
	ok := cl.Acquire(1, limit)
	assert.False(t, ok, "超出上限的 Acquire 应当被拒绝")

	// 释放一个后再次获取应成功
	cl.Release(1)
	ok = cl.Acquire(1, limit)
	assert.True(t, ok, "释放一个后再次 Acquire 应当成功")
}

// TestConcurrencyLimiter_ZeroLimitUnlimited 验证 limit=0 表示不限制，Acquire 始终返回 true。
func TestConcurrencyLimiter_ZeroLimitUnlimited(t *testing.T) {
	cl := NewConcurrencyLimiter()

	// limit=0 应当始终放行
	for i := 0; i < 100; i++ {
		ok := cl.Acquire(1, 0)
		assert.True(t, ok, "limit=0 时第 %d 次 Acquire 应当成功", i+1)
	}

	// limit=0 不会增加计数器
	assert.Equal(t, int64(0), cl.GetCount(1), "limit=0 不应创建计数器")
}

// TestConcurrencyLimiter_MultipleKeys 验证不同 keyID 拥有独立的并发计数器。
func TestConcurrencyLimiter_MultipleKeys(t *testing.T) {
	cl := NewConcurrencyLimiter()

	// key=1 上限 1，key=2 上限 2
	ok := cl.Acquire(1, 1)
	require.True(t, ok)

	ok = cl.Acquire(2, 2)
	require.True(t, ok)
	ok = cl.Acquire(2, 2)
	require.True(t, ok)

	// key=1 已满，应被拒绝
	assert.False(t, cl.Acquire(1, 1), "key=1 已达上限，应被拒绝")

	// key=2 已满，应被拒绝
	assert.False(t, cl.Acquire(2, 2), "key=2 已达上限，应被拒绝")

	// 释放 key=1 不影响 key=2
	cl.Release(1)
	assert.Equal(t, int64(0), cl.GetCount(1))
	assert.Equal(t, int64(2), cl.GetCount(2), "key=2 的计数不应受 key=1 释放的影响")

	// key=1 释放后可重新获取
	assert.True(t, cl.Acquire(1, 1))
}

// TestConcurrencyLimiter_ReleaseAfterReject 验证被拒绝的 Acquire 不会影响计数器。
func TestConcurrencyLimiter_ReleaseAfterReject(t *testing.T) {
	cl := NewConcurrencyLimiter()

	// 占满上限
	ok := cl.Acquire(1, 1)
	require.True(t, ok)
	assert.Equal(t, int64(1), cl.GetCount(1))

	// 再次获取被拒绝
	ok = cl.Acquire(1, 1)
	assert.False(t, ok, "超限 Acquire 应当被拒绝")

	// 被拒绝后计数仍为 1，没有增加
	assert.Equal(t, int64(1), cl.GetCount(1), "被拒绝的 Acquire 不应改变计数")

	// 释放后归零，确认拒绝没有产生副作用
	cl.Release(1)
	assert.Equal(t, int64(0), cl.GetCount(1), "释放后计数应归零")
}

// TestConcurrencyLimiter_ConcurrentAccess 验证高并发场景下计数器的正确性。
func TestConcurrencyLimiter_ConcurrentAccess(t *testing.T) {
	cl := NewConcurrencyLimiter()
	limit := 10
	goroutines := 100

	var (
		wg       sync.WaitGroup
		acquired int64
		mu       sync.Mutex
	)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if cl.Acquire(1, limit) {
				mu.Lock()
				acquired++
				mu.Unlock()

				// 模拟处理后释放
				cl.Release(1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(0), cl.GetCount(1), "所有 goroutine 完成后计数应归零")
	assert.Greater(t, acquired, int64(0), "至少应有一个 goroutine 成功获取")
}

// ---------------------------------------------------------------------------
// ConcurrencyMiddleware 集成测试
// ---------------------------------------------------------------------------

// TestConcurrencyMiddleware_Passes 验证 MaxConcurrent=0（无限制）时请求正常通过。
func TestConcurrencyMiddleware_Passes(t *testing.T) {
	limiter := NewConcurrencyLimiter()
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := ConcurrencyMiddleware(limiter)(inner)

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req = withResolvedKey(req, &store.DownstreamKey{
		ID:            1,
		MaxConcurrent: 0, // 不限制
		Enabled:       true,
	})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.True(t, called, "MaxConcurrent=0 时后续 handler 应被调用")
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestConcurrencyMiddleware_Enforced 验证并发数超限时返回 429 JSON 错误。
func TestConcurrencyMiddleware_Enforced(t *testing.T) {
	limiter := NewConcurrencyLimiter()

	// block 通道让第一个请求阻塞在 handler 内部，模拟长时间处理。
	block := make(chan struct{})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // 等待放行信号
		w.WriteHeader(http.StatusOK)
	})

	handler := ConcurrencyMiddleware(limiter)(inner)

	dk := &store.DownstreamKey{
		ID:            1,
		MaxConcurrent: 1,
		Enabled:       true,
	}

	// 启动第一个请求（将阻塞在 inner handler）
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		req = withResolvedKey(req, dk)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}()

	// 等待第一个请求占用并发槽位
	require.Eventually(t, func() bool {
		return limiter.GetCount(1) == 1
	}, 2*time.Second, 1*time.Millisecond, "第一个请求应占用一个并发槽位")

	// 发送第二个请求，应被拒绝
	req2 := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req2 = withResolvedKey(req2, dk)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusTooManyRequests, rec2.Code, "超出并发限制应返回 429")

	// 验证响应体是 JSON 格式的错误信息
	var body map[string]string
	err := json.NewDecoder(rec2.Body).Decode(&body)
	require.NoError(t, err, "响应体应为有效 JSON")
	assert.Equal(t, "concurrent request limit exceeded", body["error"])
	assert.Equal(t, "application/json", rec2.Header().Get("Content-Type"))

	// 放行第一个请求，确认并发计数恢复
	close(block)
	<-firstDone
	assert.Equal(t, int64(0), limiter.GetCount(1), "请求完成后并发计数应归零")
}

// TestConcurrencyMiddleware_NoResolvedKey 验证上下文中没有 ResolvedKey 时请求正常通过（不 panic）。
func TestConcurrencyMiddleware_NoResolvedKey(t *testing.T) {
	limiter := NewConcurrencyLimiter()
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := ConcurrencyMiddleware(limiter)(inner)

	// 不注入 ResolvedKey
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.True(t, called, "无 ResolvedKey 时后续 handler 应被调用")
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestConcurrencyMiddleware_NegativeMaxConcurrent 验证 MaxConcurrent 为负数时等同于不限制。
func TestConcurrencyMiddleware_NegativeMaxConcurrent(t *testing.T) {
	limiter := NewConcurrencyLimiter()
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := ConcurrencyMiddleware(limiter)(inner)

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req = withResolvedKey(req, &store.DownstreamKey{
		ID:            1,
		MaxConcurrent: -1,
		Enabled:       true,
	})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.True(t, called, "MaxConcurrent<0 时后续 handler 应被调用")
	assert.Equal(t, http.StatusOK, rec.Code)
}
