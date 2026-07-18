package middleware

import (
	"sort"
	"sync/atomic"

	"github.com/Instawork/llm-proxy/internal/store"
)

type fullRecordingSnapshot struct {
	enabled bool
	allKeys bool
	keyIDs  map[int64]struct{}
}

// FullRecordingPolicy 提供无锁读取的全量记录运行时策略。
type FullRecordingPolicy struct {
	snapshot atomic.Value
}

func NewFullRecordingPolicy(config store.FullRecordingConfig) *FullRecordingPolicy {
	policy := &FullRecordingPolicy{}
	policy.Update(config)
	return policy
}

// Update 用不可变快照替换当前策略，避免请求热路径访问数据库或争用互斥锁。
func (p *FullRecordingPolicy) Update(config store.FullRecordingConfig) {
	keyIDs := make(map[int64]struct{}, len(config.DownstreamKeyIDs))
	for _, id := range config.DownstreamKeyIDs {
		keyIDs[id] = struct{}{}
	}
	p.snapshot.Store(fullRecordingSnapshot{enabled: config.Enabled, allKeys: config.AllKeys, keyIDs: keyIDs})
}

func (p *FullRecordingPolicy) ShouldRecord(keyID int64) bool {
	if p == nil {
		return false
	}
	snapshot := p.snapshot.Load().(fullRecordingSnapshot)
	if !snapshot.enabled {
		return false
	}
	if snapshot.allKeys {
		return true
	}
	_, ok := snapshot.keyIDs[keyID]
	return ok
}

func (p *FullRecordingPolicy) Config() store.FullRecordingConfig {
	if p == nil {
		return store.FullRecordingConfig{}
	}
	snapshot := p.snapshot.Load().(fullRecordingSnapshot)
	config := store.FullRecordingConfig{Enabled: snapshot.enabled, AllKeys: snapshot.allKeys}
	for id := range snapshot.keyIDs {
		config.DownstreamKeyIDs = append(config.DownstreamKeyIDs, id)
	}
	sort.Slice(config.DownstreamKeyIDs, func(i, j int) bool {
		return config.DownstreamKeyIDs[i] < config.DownstreamKeyIDs[j]
	})
	return config
}
