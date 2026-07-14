package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"

	"github.com/Instawork/llm-proxy/internal/store"
)

// ResolvedKey 包含中间件所需的下游 Key 数据。
type ResolvedKey struct {
	ID       int64
	Name     string
	RPMLimit int
	Enabled  bool
}

// KeySnapshot 是一个从 key_hash 到 ResolvedKey 的不可变映射。
type KeySnapshot struct {
	keys map[string]*ResolvedKey
}

func (s *KeySnapshot) Lookup(hash string) *ResolvedKey {
	if s == nil {
		return nil
	}
	return s.keys[hash]
}

// KeyCache 持有指向当前 KeySnapshot 的原子引用。
type KeyCache struct {
	snapshot atomic.Value // 存储 *KeySnapshot
}

func NewKeyCache() *KeyCache {
	kc := &KeyCache{}
	kc.snapshot.Store(&KeySnapshot{keys: make(map[string]*ResolvedKey)})
	return kc
}

// Reload 基于 store 中的全部 Key 构建新快照，并原子替换当前快照。
func (kc *KeyCache) Reload(s *store.Store) error {
	allKeys, err := s.GetAllKeys()
	if err != nil {
		return err
	}

	m := make(map[string]*ResolvedKey, len(allKeys))
	for _, dk := range allKeys {
		m[dk.KeyHash] = &ResolvedKey{
			ID:       dk.ID,
			Name:     dk.Name,
			RPMLimit: dk.RPMLimit,
			Enabled:  dk.Enabled,
		}
	}

	kc.snapshot.Store(&KeySnapshot{keys: m})
	return nil
}

func (kc *KeyCache) get() *KeySnapshot {
	return kc.snapshot.Load().(*KeySnapshot)
}

// KeyResolverMiddleware 用上下文中的 key hash 在原子快照中查找对应 Key。
// 未知或已禁用的 Key 会被 401 拒绝。
func KeyResolverMiddleware(cache *KeyCache) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hash := KeyHashFromContext(r.Context())
			resolved := cache.get().Lookup(hash)

			if resolved == nil || !resolved.Enabled {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid or disabled API key"}) //nolint:errcheck
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyResolvedKey, &store.DownstreamKey{
				ID:       resolved.ID,
				KeyHash:  hash,
				Name:     resolved.Name,
				RPMLimit: resolved.RPMLimit,
				Enabled:  resolved.Enabled,
			})
			ctx = context.WithValue(ctx, ctxKeyDownstreamKeyID, resolved.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
