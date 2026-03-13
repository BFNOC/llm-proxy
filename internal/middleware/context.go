package middleware

import (
	"context"

	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
)

type contextKey string

const (
	ctxKeyStyle           contextKey = "provider_style"
	ctxKeyHash            contextKey = "key_hash"
	ctxKeyResolvedKey     contextKey = "resolved_key"
	ctxKeyDownstreamKeyID contextKey = "downstream_key_id"
)

func StyleFromContext(ctx context.Context) proxy.ProviderStyle {
	v, _ := ctx.Value(ctxKeyStyle).(proxy.ProviderStyle)
	return v
}

func KeyHashFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyHash).(string)
	return v
}

func ResolvedKeyFromContext(ctx context.Context) *store.DownstreamKey {
	v, _ := ctx.Value(ctxKeyResolvedKey).(*store.DownstreamKey)
	return v
}

func DownstreamKeyIDFromContext(ctx context.Context) int64 {
	v, _ := ctx.Value(ctxKeyDownstreamKeyID).(int64)
	return v
}
