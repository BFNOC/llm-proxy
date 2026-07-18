package proxy

import (
	"context"
)

// allowedUpstreamIDsKey 使用私有空结构体作为 context key，
// 避免字符串 key 冲突，也避免外部代码通过同名字符串伪造绑定结果。
type allowedUpstreamIDsKey struct{}

// ContextWithAllowedUpstreamIDs 把当前请求允许访问的上游 ID 集合写入 context。
// 绑定关系使用稳定的数据库 ID，而不是名称或 URL，避免上游重命名后授权漂移。
func ContextWithAllowedUpstreamIDs(ctx context.Context, ids []int64) context.Context {
	return context.WithValue(ctx, allowedUpstreamIDsKey{}, ids)
}

// AllowedUpstreamIDsFromContext 读取上游访问白名单。
func AllowedUpstreamIDsFromContext(ctx context.Context) []int64 {
	v, _ := ctx.Value(allowedUpstreamIDsKey{}).([]int64)
	return v
}

// ---------------------------------------------------------------------------
// Per-Key 模型覆盖的 context helper
// ---------------------------------------------------------------------------

type keyModelOverridesKey struct{}

// KeyModelOverrideRule 是一条运行时覆盖规则。
// 一个 ModelPattern 可以映射到多个 UpstreamIDs（故障切换列表）。
type KeyModelOverrideRule struct {
	ModelPattern string
	UpstreamIDs  []int64
}

// ContextWithKeyModelOverrides 把 per-key 模型路由覆盖写入 context。
func ContextWithKeyModelOverrides(ctx context.Context, overrides []KeyModelOverrideRule) context.Context {
	return context.WithValue(ctx, keyModelOverridesKey{}, overrides)
}

// KeyModelOverridesFromContext 读取 per-key 模型路由覆盖。
func KeyModelOverridesFromContext(ctx context.Context) []KeyModelOverrideRule {
	v, _ := ctx.Value(keyModelOverridesKey{}).([]KeyModelOverrideRule)
	return v
}
