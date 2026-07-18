package proxy

import (
	"net/url"
	"sync"
)

// ActiveUpstream 保存当前可用的上游端点信息。
type ActiveUpstream struct {
	// ID 对应 upstream_providers 表主键，用于把运行时健康列表和持久化绑定关系做稳定关联。
	ID            int64
	BaseURL       *url.URL
	APIKeys       []string // 支持多个 API Key，通过 NextAPIKey() 选取（仅含已启用的 Key）
	KeyRowIDs     []int64  // 对应的数据库行 ID，与 APIKeys 一一对应
	Name          string
	ProxyURL      string   // 可选代理地址，空表示继承环境代理
	ModelPatterns []string // 支持的模型 glob 模式，空表示接受所有模型
	// KeySchedulingMode 控制多 Key 调度策略："round-robin"（默认）或 "fill"。
	KeySchedulingMode string
	// AuthMode 控制 Anthropic 鉴权头：api_key（x-api-key）或 oauth（Authorization: Bearer）。
	AuthMode string
	// WebSocketEnabled 表示该上游是否支持 WebSocket（如 OpenAI Realtime API）。
	WebSocketEnabled bool

	// UpstreamRPMLimit 上游每分钟请求限制，0 表示不限制。
	UpstreamRPMLimit int
	// CircuitBreakerThreshold 连续失败多少次后触发熔断，0 表示不启用。
	CircuitBreakerThreshold int
	// CircuitBreakerRecoverySeconds 熔断后恢复探测的间隔秒数。
	CircuitBreakerRecoverySeconds int

	keyMu         sync.Mutex
	keyIndex      int  // round-robin 索引
	fillKeyIndex  int  // fill 模式当前使用的 Key 索引
	fillKeyFailed bool // fill 模式当前 Key 是否已失败

	// 失败追踪：记录每个 Key 的连续失败次数，用于自动禁用。
	keyFailures map[int]int64 // keyRowID -> 连续失败次数
}

// NextAPIKey 返回下一个 API Key、其在列表中的索引（0-based）和对应的数据库行 ID。
// 调度策略由 KeySchedulingMode 决定。
func (u *ActiveUpstream) NextAPIKey() (string, int, int64) {
	if len(u.APIKeys) == 0 {
		return "", -1, -1
	}
	if len(u.APIKeys) == 1 {
		rowID := int64(-1)
		if len(u.KeyRowIDs) > 0 {
			rowID = u.KeyRowIDs[0]
		}
		return u.APIKeys[0], 0, rowID
	}
	u.keyMu.Lock()
	defer u.keyMu.Unlock()

	switch u.KeySchedulingMode {
	case "fill":
		return u.nextAPIKeyFill()
	default:
		return u.nextAPIKeyRoundRobin()
	}
}

// nextAPIKeyRoundRobin 依次轮询每个 Key。
func (u *ActiveUpstream) nextAPIKeyRoundRobin() (string, int, int64) {
	idx := u.keyIndex % len(u.APIKeys)
	u.keyIndex++
	rowID := int64(-1)
	if idx < len(u.KeyRowIDs) {
		rowID = u.KeyRowIDs[idx]
	}
	return u.APIKeys[idx], idx, rowID
}

// nextAPIKeyFill 优先使用当前 Key 直到出错，再切换到下一个。
func (u *ActiveUpstream) nextAPIKeyFill() (string, int, int64) {
	if u.fillKeyFailed || u.fillKeyIndex >= len(u.APIKeys) {
		// 切换到下一个 Key
		u.fillKeyIndex = (u.fillKeyIndex + 1) % len(u.APIKeys)
		u.fillKeyFailed = false
	}
	rowID := int64(-1)
	if u.fillKeyIndex < len(u.KeyRowIDs) {
		rowID = u.KeyRowIDs[u.fillKeyIndex]
	}
	return u.APIKeys[u.fillKeyIndex], u.fillKeyIndex, rowID
}

// MarkKeyFailed 在 fill 模式下标记当前 Key 失败，下次调用 NextAPIKey() 时切换。
func (u *ActiveUpstream) MarkKeyFailed() {
	u.keyMu.Lock()
	u.fillKeyFailed = true
	u.keyMu.Unlock()
}
