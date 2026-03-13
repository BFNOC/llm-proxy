package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"

	"github.com/Instawork/llm-proxy/internal/store"
)

// ResolvedKey contains the downstream key data needed by middleware.
type ResolvedKey struct {
	ID       int64
	Name     string
	RPMLimit int
	Enabled  bool
}

// KeySnapshot is an immutable map from key_hash -> ResolvedKey.
type KeySnapshot struct {
	keys map[string]*ResolvedKey
}

func (s *KeySnapshot) Lookup(hash string) *ResolvedKey {
	if s == nil {
		return nil
	}
	return s.keys[hash]
}

// KeyCache holds an atomic reference to the current KeySnapshot.
type KeyCache struct {
	snapshot atomic.Value // stores *KeySnapshot
}

func NewKeyCache() *KeyCache {
	kc := &KeyCache{}
	kc.snapshot.Store(&KeySnapshot{keys: make(map[string]*ResolvedKey)})
	return kc
}

// Reload builds a new snapshot from all keys in the store and atomically swaps it in.
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

// KeyResolverMiddleware looks up the key hash from the context in the atomic snapshot.
// Requests with an unknown or disabled key are rejected with 401.
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
