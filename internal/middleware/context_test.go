package middleware

import (
	"context"
	"testing"

	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/stretchr/testify/assert"
)

func TestStyleFromContext(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		expected proxy.ProviderStyle
	}{
		{"openai style", proxy.StyleOpenAI, proxy.StyleOpenAI},
		{"anthropic style", proxy.StyleAnthropic, proxy.StyleAnthropic},
		{"empty context returns zero value", nil, ""},
		{"wrong type returns zero value", 42, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.value != nil {
				ctx = context.WithValue(ctx, ctxKeyStyle, tt.value)
			}
			got := StyleFromContext(ctx)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestKeyHashFromContext(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		expected string
	}{
		{"valid hash", "abc123def456", "abc123def456"},
		{"empty context returns empty string", nil, ""},
		{"wrong type returns empty string", 12345, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.value != nil {
				ctx = context.WithValue(ctx, ctxKeyHash, tt.value)
			}
			got := KeyHashFromContext(ctx)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestResolvedKeyFromContext(t *testing.T) {
	t.Run("returns nil when not set", func(t *testing.T) {
		ctx := context.Background()
		got := ResolvedKeyFromContext(ctx)
		assert.Nil(t, got)
	})

	t.Run("returns nil for wrong type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ctxKeyResolvedKey, "not-a-key")
		got := ResolvedKeyFromContext(ctx)
		assert.Nil(t, got)
	})

	t.Run("returns key when set", func(t *testing.T) {
		dk := &store.DownstreamKey{ID: 7, Name: "test-key", Enabled: true}
		ctx := context.WithValue(context.Background(), ctxKeyResolvedKey, dk)
		got := ResolvedKeyFromContext(ctx)
		assert.Equal(t, dk, got)
	})
}

func TestDownstreamKeyIDFromContext(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		expected int64
	}{
		{"valid ID", int64(42), int64(42)},
		{"empty context returns zero", nil, 0},
		{"wrong type returns zero", "42", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.value != nil {
				ctx = context.WithValue(ctx, ctxKeyDownstreamKeyID, tt.value)
			}
			got := DownstreamKeyIDFromContext(ctx)
			assert.Equal(t, tt.expected, got)
		})
	}
}
