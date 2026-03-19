package middleware

import (
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// GlobalRequestCounter — 全局 RPM / RPS 计数器
// ---------------------------------------------------------------------------

// bucket 是环形缓冲区中的一个时间桶。
// 使用 mutex 保护 epoch+count 的原子性，避免 CAS 竞态丢计数。
type bucket struct {
	mu    sync.Mutex
	epoch int64 // 该桶对应的 Unix 秒
	count int64
}

// increment 对桶执行一次计数。如果桶的 epoch 已过期则先重置。
func (b *bucket) increment(now int64) {
	b.mu.Lock()
	if b.epoch == now {
		b.count++
	} else {
		// 桶属于旧秒，重置
		b.epoch = now
		b.count = 1
	}
	b.mu.Unlock()
}

// read 安全读取桶的 epoch 和 count。
func (b *bucket) read() (epoch, count int64) {
	b.mu.Lock()
	epoch, count = b.epoch, b.count
	b.mu.Unlock()
	return
}

// GlobalRequestCounter 使用 60 个 bucket 的环形缓冲区统计 RPM/RPS。
// 每个 bucket 保存 1 秒内的请求数，用各自的 mutex 保护，
// 不同秒的请求不会互相阻塞。
type GlobalRequestCounter struct {
	buckets [60]bucket
	// startTime 用于冷启动时计算实际 elapsed 秒数，避免 RPS 虚高
	startTime time.Time
}

// NewGlobalRequestCounter 创建一个全局请求计数器。
func NewGlobalRequestCounter() *GlobalRequestCounter {
	return &GlobalRequestCounter{
		startTime: time.Now(),
	}
}

// Increment 记录一次请求。每个桶有独立 mutex，不同秒不争用。
func (c *GlobalRequestCounter) Increment() {
	now := time.Now().Unix()
	idx := now % 60
	c.buckets[idx].increment(now)
}

// RPM 返回过去 60 秒内的总请求数。
func (c *GlobalRequestCounter) RPM() int {
	now := time.Now().Unix()
	var total int64
	for i := 0; i < 60; i++ {
		epoch, count := c.buckets[i].read()
		if now-epoch < 60 {
			total += count
		}
	}
	return int(total)
}

// RPS 返回过去若干秒的平均每秒请求数。
// 冷启动阶段（<5s）用实际 elapsed 做分母，避免虚高。
// 包含当前秒的部分计数，以减少面板的滞后感。
func (c *GlobalRequestCounter) RPS() float64 {
	now := time.Now()
	elapsed := now.Sub(c.startTime).Seconds()

	// 取样窗口：正常 5 秒，冷启动用实际运行时间
	window := 5.0
	if elapsed < window {
		window = elapsed
	}
	if window < 1 {
		window = 1
	}

	nowUnix := now.Unix()
	var total int64
	windowInt := int(window)
	// 包含当前秒（s=0）以减少滞后
	for s := 0; s < windowInt; s++ {
		sec := nowUnix - int64(s)
		idx := sec % 60
		epoch, count := c.buckets[idx].read()
		if epoch == sec {
			total += count
		}
	}
	return float64(total) / window
}

// ---------------------------------------------------------------------------
// PerKeyStatsCollector — 每 Key RPM 统计（独立于限流器）
// ---------------------------------------------------------------------------

// keyCounter 是单个 Key 的环形缓冲区，结构与全局计数器相同。
type keyCounter struct {
	buckets  [60]bucket
	lastSeen int64 // Unix 秒，用于过期清理
}

func (kc *keyCounter) record(now int64) {
	atomic.StoreInt64(&kc.lastSeen, now)
	idx := now % 60
	kc.buckets[idx].increment(now)
}

func (kc *keyCounter) rpm(now int64) int {
	var total int64
	for i := 0; i < 60; i++ {
		epoch, count := kc.buckets[i].read()
		if now-epoch < 60 {
			total += count
		}
	}
	return int(total)
}

// PerKeyStatsCollector 独立于 PerKeyRPMLimiter，统计每个 Key 的实时 RPM。
// 限流器只为限速的 Key 记录时间戳，不限速 Key 不记录；
// 而 StatsCollector 对所有已鉴权请求都会计数。
type PerKeyStatsCollector struct {
	mu   sync.RWMutex
	keys map[int64]*keyCounter
}

// NewPerKeyStatsCollector 创建 per-key 统计收集器。
func NewPerKeyStatsCollector() *PerKeyStatsCollector {
	return &PerKeyStatsCollector{
		keys: make(map[int64]*keyCounter),
	}
}

// Record 记录某个 Key 的一次请求。
func (s *PerKeyStatsCollector) Record(keyID int64) {
	now := time.Now().Unix()

	// 快路径：读锁取已有 counter
	s.mu.RLock()
	kc, ok := s.keys[keyID]
	s.mu.RUnlock()

	if ok {
		kc.record(now)
		return
	}

	// 慢路径：写锁创建新 counter
	s.mu.Lock()
	// 双重检查
	kc, ok = s.keys[keyID]
	if !ok {
		kc = &keyCounter{}
		s.keys[keyID] = kc
	}
	s.mu.Unlock()

	kc.record(now)
}

// GetKeyRPM 返回单个 Key 的当前 RPM。
func (s *PerKeyStatsCollector) GetKeyRPM(keyID int64) int {
	s.mu.RLock()
	kc, ok := s.keys[keyID]
	s.mu.RUnlock()
	if !ok {
		return 0
	}
	return kc.rpm(time.Now().Unix())
}

// AllActiveRPMs 返回最近 60 秒内有活动的所有 Key 的 RPM。
// 同时清理超过 5 分钟无活动的 Key，防止内存泄漏。
func (s *PerKeyStatsCollector) AllActiveRPMs() map[int64]int {
	now := time.Now().Unix()
	result := make(map[int64]int)
	var staleKeys []int64

	s.mu.RLock()
	for keyID, kc := range s.keys {
		lastSeen := atomic.LoadInt64(&kc.lastSeen)
		if now-lastSeen > 300 { // 5 分钟无活动
			staleKeys = append(staleKeys, keyID)
			continue
		}
		if rpm := kc.rpm(now); rpm > 0 {
			result[keyID] = rpm
		}
	}
	s.mu.RUnlock()

	// 清理过期 key（需要写锁，但不在热路径上）
	if len(staleKeys) > 0 {
		s.mu.Lock()
		for _, keyID := range staleKeys {
			// 再次检查，防止清理期间有新请求
			if kc, ok := s.keys[keyID]; ok {
				if now-atomic.LoadInt64(&kc.lastSeen) > 300 {
					delete(s.keys, keyID)
				}
			}
		}
		s.mu.Unlock()
	}

	return result
}

// RemoveKey 清理已删除的 Key 的统计数据。
func (s *PerKeyStatsCollector) RemoveKey(keyID int64) {
	s.mu.Lock()
	delete(s.keys, keyID)
	s.mu.Unlock()
}
