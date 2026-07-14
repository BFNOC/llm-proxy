package middleware

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// 熔断器单元测试
// ---------------------------------------------------------------------------

// TestCircuitBreaker_ClosedState 新建熔断器默认为 Closed，IsAvailable 返回 true
func TestCircuitBreaker_ClosedState(t *testing.T) {
	cb := NewCircuitBreaker()

	// 未记录任何事件的上游，默认为可用（Closed）
	assert.True(t, cb.IsAvailable(1), "新建熔断器应可用")
	assert.Equal(t, CircuitClosed, cb.GetState(1))
}

// TestCircuitBreaker_OpenAfterThreshold 连续失败 N 次后应切换为 Open，IsAvailable 返回 false
func TestCircuitBreaker_OpenAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker()

	threshold := 3
	recovery := 60

	// 连续失败 threshold 次
	for i := 0; i < threshold; i++ {
		cb.RecordFailure(1, threshold, recovery)
	}

	assert.Equal(t, CircuitOpen, cb.GetState(1), "达到阈值后应为 Open")
	assert.False(t, cb.IsAvailable(1), "Open 状态下应不可用")
}

// TestCircuitBreaker_RecoveryToHalfOpen Open 状态经过恢复时间后应切换为 HalfOpen
func TestCircuitBreaker_RecoveryToHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker()

	threshold := 2
	recoverySeconds := 1 // 1 秒恢复时间，方便测试

	// 触发熔断
	for i := 0; i < threshold; i++ {
		cb.RecordFailure(1, threshold, recoverySeconds)
	}
	assert.Equal(t, CircuitOpen, cb.GetState(1))

	// 等待恢复时间
	time.Sleep(time.Duration(recoverySeconds)*time.Second + 100*time.Millisecond)

	// IsAvailable 应触发状态转换到 HalfOpen 并返回 true（允许一个探测请求）
	assert.True(t, cb.IsAvailable(1), "恢复时间到后应允许探测请求")
	assert.Equal(t, CircuitHalfOpen, cb.GetState(1), "应转换为 HalfOpen")

	// 第二次调用 IsAvailable 应返回 false（HalfOpen 只允许一个探测请求）
	assert.False(t, cb.IsAvailable(1), "HalfOpen 状态下不应允许更多请求")
}

// TestCircuitBreaker_SuccessResetsToClose HalfOpen 状态下成功请求应重置为 Closed
func TestCircuitBreaker_SuccessResetsToClose(t *testing.T) {
	cb := NewCircuitBreaker()

	threshold := 2
	recoverySeconds := 1

	// 先触发熔断
	for i := 0; i < threshold; i++ {
		cb.RecordFailure(1, threshold, recoverySeconds)
	}
	assert.Equal(t, CircuitOpen, cb.GetState(1))

	// 等待恢复
	time.Sleep(time.Duration(recoverySeconds)*time.Second + 100*time.Millisecond)

	// 触发 HalfOpen
	assert.True(t, cb.IsAvailable(1))
	assert.Equal(t, CircuitHalfOpen, cb.GetState(1))

	// 记录成功，应重置为 Closed
	cb.RecordSuccess(1)
	assert.Equal(t, CircuitClosed, cb.GetState(1), "成功后应重置为 Closed")
	assert.True(t, cb.IsAvailable(1), "Closed 状态下应可用")
}

// TestCircuitBreaker_GetAllStates 多个上游应各自维护独立的熔断状态
func TestCircuitBreaker_GetAllStates(t *testing.T) {
	cb := NewCircuitBreaker()

	// 上游 1：触发熔断
	cb.RecordFailure(1, 2, 60)
	cb.RecordFailure(1, 2, 60)

	// 上游 2：正常（只有一次失败，未达阈值）
	cb.RecordFailure(2, 3, 60)

	// 上游 3：未记录任何事件

	states := cb.GetAllStates()

	assert.Equal(t, CircuitOpen, states[1], "上游 1 应为 Open")
	assert.Equal(t, CircuitClosed, states[2], "上游 2 应为 Closed（未达阈值）")
	// 上游 3 不在 map 中，GetState 返回 Closed
	assert.Equal(t, CircuitClosed, cb.GetState(3), "上游 3 默认应为 Closed")
}
