package middleware

import (
	"sync"
	"time"
)

// CircuitState 表示熔断器的三种状态。
type CircuitState int

const (
	// CircuitClosed 正常状态，允许所有请求通过。
	CircuitClosed CircuitState = iota
	// CircuitOpen 熔断状态，拒绝所有请求。
	CircuitOpen
	// CircuitHalfOpen 半开状态，允许一个探测请求通过以测试恢复。
	CircuitHalfOpen
)

// String 返回状态的可读名称。
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// circuit 保存单个上游的熔断器状态。
type circuit struct {
	state           CircuitState
	failures        int
	threshold       int
	recoverySeconds int
	lastFailure     time.Time
	lastSuccess     time.Time
}

// CircuitBreaker 为每个上游维护独立的熔断器状态。
type CircuitBreaker struct {
	mu       sync.RWMutex
	circuits map[int64]*circuit // upstreamID -> 熔断器状态
}

// NewCircuitBreaker 创建一个新的熔断器管理器。
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		circuits: make(map[int64]*circuit),
	}
}

// RecordSuccess 记录成功请求：重置失败计数器，将状态设置为 Closed。
func (cb *CircuitBreaker) RecordSuccess(upstreamID int64) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c, ok := cb.circuits[upstreamID]
	if !ok {
		// 之前没有记录，无需操作
		return
	}
	c.failures = 0
	c.state = CircuitClosed
	c.lastSuccess = time.Now()
}

// RecordFailure 记录失败请求：递增失败计数器。
// 如果连续失败次数 >= threshold，则将状态切换为 Open。
// threshold <= 0 表示不启用熔断。
func (cb *CircuitBreaker) RecordFailure(upstreamID int64, threshold, recoverySeconds int) {
	if threshold <= 0 {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	c, ok := cb.circuits[upstreamID]
	if !ok {
		c = &circuit{
			state:           CircuitClosed,
			threshold:       threshold,
			recoverySeconds: recoverySeconds,
		}
		cb.circuits[upstreamID] = c
	}
	// 更新配置（可能已在线修改）
	c.threshold = threshold
	c.recoverySeconds = recoverySeconds

	c.failures++
	c.lastFailure = time.Now()

	if c.failures >= threshold {
		c.state = CircuitOpen
	}
}

// IsAvailable 判断上游是否可以接受请求。
//   - Closed: 返回 true
//   - Open: 如果恢复时间已过，切换到 HalfOpen 并返回 true（允许一个探测请求）
//   - HalfOpen: 返回 false（已有一个探测请求正在进行）
func (cb *CircuitBreaker) IsAvailable(upstreamID int64) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c, ok := cb.circuits[upstreamID]
	if !ok {
		// 没有记录，默认可用
		return true
	}

	switch c.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		recovery := time.Duration(c.recoverySeconds) * time.Second
		if recovery <= 0 {
			recovery = 30 * time.Second // 默认 30 秒恢复时间
		}
		if time.Since(c.lastFailure) >= recovery {
			// 恢复时间已过，切换到半开状态，允许一个探测请求
			c.state = CircuitHalfOpen
			return true
		}
		return false
	case CircuitHalfOpen:
		// 已有探测请求在进行中，不允许更多请求
		return false
	default:
		return true
	}
}

// GetState 返回指定上游的当前熔断器状态。
func (cb *CircuitBreaker) GetState(upstreamID int64) CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	c, ok := cb.circuits[upstreamID]
	if !ok {
		return CircuitClosed
	}
	return c.state
}

// GetAllStates 返回所有上游的熔断器状态，用于管理面板展示。
func (cb *CircuitBreaker) GetAllStates() map[int64]CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	result := make(map[int64]CircuitState, len(cb.circuits))
	for id, c := range cb.circuits {
		result[id] = c.state
	}
	return result
}
