package middleware

import (
	"sync"
	"time"
)

// upstreamRPMShards 上游 RPM 限流器的分片数量，与 per-Key 限流器保持一致。
const upstreamRPMShards = 64

type upstreamRPMShard struct {
	mu      sync.Mutex
	buckets map[int64]*slidingWindow
}

// UpstreamRPMLimiter 使用滑动窗口跟踪每个上游每分钟的请求数。
// 与 PerKeyRPMLimiter 使用相同的分片 + 滑动窗口模式，但以上游 ID 为键。
type UpstreamRPMLimiter struct {
	shards [upstreamRPMShards]upstreamRPMShard
	stopGC chan struct{}
	gcOnce sync.Once
}

func (l *UpstreamRPMLimiter) shard(upstreamID int64) *upstreamRPMShard {
	idx := upstreamID % upstreamRPMShards
	if idx < 0 {
		idx = -idx
	}
	return &l.shards[idx]
}

// NewUpstreamRPMLimiter 创建一个新的上游 RPM 限流器并启动后台 GC。
func NewUpstreamRPMLimiter() *UpstreamRPMLimiter {
	l := &UpstreamRPMLimiter{
		stopGC: make(chan struct{}),
	}
	for i := range l.shards {
		l.shards[i].buckets = make(map[int64]*slidingWindow)
	}
	go l.gcLoop()
	return l
}

func (l *UpstreamRPMLimiter) gcLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.GC()
		case <-l.stopGC:
			return
		}
	}
}

// GC 清除最近一分钟内没有时间戳的空闲窗口。
func (l *UpstreamRPMLimiter) GC() {
	now := time.Now()
	for i := range l.shards {
		s := &l.shards[i]
		s.mu.Lock()
		for id, sw := range s.buckets {
			if sw.countInWindow(now) == 0 {
				delete(s.buckets, id)
			}
		}
		s.mu.Unlock()
	}
}

// StopGC 停止后台 GC goroutine（用于测试/停机）。
func (l *UpstreamRPMLimiter) StopGC() {
	l.gcOnce.Do(func() {
		close(l.stopGC)
	})
}

// Allow 检查上游是否还有 RPM 配额。返回 true 表示允许。
// limit <= 0 表示不限制。
func (l *UpstreamRPMLimiter) Allow(upstreamID int64, limit int) bool {
	if limit <= 0 {
		return true
	}

	s := l.shard(upstreamID)
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	sw, ok := s.buckets[upstreamID]
	if !ok {
		sw = &slidingWindow{}
		s.buckets[upstreamID] = sw
	}

	count := sw.countInWindow(now)
	if count >= limit {
		return false
	}

	sw.add(now)
	return true
}

// GetRPM 返回指定上游当前分钟内的请求数。
func (l *UpstreamRPMLimiter) GetRPM(upstreamID int64) int {
	s := l.shard(upstreamID)
	s.mu.Lock()
	defer s.mu.Unlock()

	sw, ok := s.buckets[upstreamID]
	if !ok {
		return 0
	}
	return sw.countInWindow(time.Now())
}
