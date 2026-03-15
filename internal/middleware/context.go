package middleware

import (
	"context"

	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
)

// contextKey 使用包内私有类型而不是裸 string，避免和其他中间件或第三方库的 context key 冲突。
type contextKey string

const (
	ctxKeyStyle           contextKey = "provider_style"
	ctxKeyHash            contextKey = "key_hash"
	ctxKeyResolvedKey     contextKey = "resolved_key"
	ctxKeyDownstreamKeyID contextKey = "downstream_key_id"
)

// StyleFromContext 读取请求分类阶段识别出的 provider 风格。
// 读取失败时返回零值，让后续流程按"尚未分类"处理，而不是因类型断言失败中断请求。
func StyleFromContext(ctx context.Context) proxy.ProviderStyle {
	v, _ := ctx.Value(ctxKeyStyle).(proxy.ProviderStyle)
	return v
}

// KeyHashFromContext 返回请求里提取出的下游 Key 哈希。
// 中间件链只传递哈希而不是明文 Key，可以缩小敏感信息在内存中的扩散范围。
func KeyHashFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyHash).(string)
	return v
}

// ResolvedKeyFromContext 返回已解析出的下游 Key 快照。
// 返回 nil 表示当前请求尚未完成鉴权解析，调用方可以据此决定是否继续做限流或审计。
func ResolvedKeyFromContext(ctx context.Context) *store.DownstreamKey {
	v, _ := ctx.Value(ctxKeyResolvedKey).(*store.DownstreamKey)
	return v
}

// DownstreamKeyIDFromContext 返回下游 Key 的数据库主键。
// 单独传递 ID 可以让绑定、审计和限流逻辑共享稳定标识，而不依赖完整对象。
func DownstreamKeyIDFromContext(ctx context.Context) int64 {
	v, _ := ctx.Value(ctxKeyDownstreamKeyID).(int64)
	return v
}
