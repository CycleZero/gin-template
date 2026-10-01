package cache

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// memoryEntry 内存缓存条目
type memoryEntry struct {
	val       []byte
	expiresAt time.Time // 零值表示永不过期
}

// MemoryCache 内存缓存实现（cache.Cache 接口）
// 适用于单元测试与本地开发（无 Redis 环境），接口文档中预留的"内存"实现。
// 生产环境请使用 RedisCache（灰度期与 Laravel 共享 Redis key）。
type MemoryCache struct {
	mu    sync.RWMutex
	data  map[string]memoryEntry
	zsets map[string]map[string]float64 // 有序集合：key → member → score（模拟 Redis ZSET）
}

// NewMemoryCache 创建内存缓存
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		data:  make(map[string]memoryEntry),
		zsets: make(map[string]map[string]float64),
	}
}

// expired 判断条目是否已过期（调用方须持有读锁）
func (m *MemoryCache) expired(e memoryEntry) bool {
	return !e.expiresAt.IsZero() && time.Now().After(e.expiresAt)
}

// marshal 序列化存储值：[]byte 原样存储，其余 JSON 序列化
func marshalValue(value any) ([]byte, error) {
	if b, ok := value.([]byte); ok {
		return b, nil
	}
	return json.Marshal(value)
}

// Get 获取缓存
func (m *MemoryCache) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.data[key]
	if !ok || m.expired(e) {
		return nil, ErrKeyNotFound
	}
	return e.val, nil
}

// GetObject 获取缓存并反序列化为对象
func (m *MemoryCache) GetObject(ctx context.Context, key string, value any) error {
	raw, err := m.Get(ctx, key)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, value)
}

// MGet 批量获取缓存
func (m *MemoryCache) MGet(_ context.Context, keys []string) (map[string][]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string][]byte, len(keys))
	for _, k := range keys {
		if e, ok := m.data[k]; ok && !m.expired(e) {
			out[k] = e.val
		}
	}
	return out, nil
}

// Set 设置缓存，expiration=0 表示永不过期
func (m *MemoryCache) Set(_ context.Context, key string, value any, expiration time.Duration) error {
	raw, err := marshalValue(value)
	if err != nil {
		return err
	}
	var expiresAt time.Time
	if expiration > 0 {
		expiresAt = time.Now().Add(expiration)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = memoryEntry{val: raw, expiresAt: expiresAt}
	return nil
}

// SetNX 仅当键不存在时设置缓存
func (m *MemoryCache) SetNX(_ context.Context, key string, value any, expiration time.Duration) (bool, error) {
	raw, err := marshalValue(value)
	if err != nil {
		return false, err
	}
	var expiresAt time.Time
	if expiration > 0 {
		expiresAt = time.Now().Add(expiration)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.data[key]; ok && !m.expired(e) {
		return false, nil
	}
	m.data[key] = memoryEntry{val: raw, expiresAt: expiresAt}
	return true, nil
}

// Delete 删除缓存
func (m *MemoryCache) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

// Exists 判断键是否存在
func (m *MemoryCache) Exists(_ context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.data[key]
	return ok && !m.expired(e), nil
}

// TTL 获取键剩余过期时间
func (m *MemoryCache) TTL(_ context.Context, key string) (time.Duration, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.data[key]
	if !ok || m.expired(e) {
		return -2 * time.Second, nil
	}
	if e.expiresAt.IsZero() {
		return -1 * time.Second, nil
	}
	return time.Until(e.expiresAt), nil
}

// Expire 设置键的过期时间
func (m *MemoryCache) Expire(_ context.Context, key string, expiration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.data[key]
	if !ok || m.expired(e) {
		return ErrKeyNotFound
	}
	e.expiresAt = time.Now().Add(expiration)
	m.data[key] = e
	return nil
}

// Incr 原子递增 1
func (m *MemoryCache) Incr(_ context.Context, key string) (int64, error) {
	return m.incrBy(key, 1)
}

// Decr 原子递减 1
func (m *MemoryCache) Decr(_ context.Context, key string) (int64, error) {
	return m.incrBy(key, -1)
}

// incrBy 原子增减（保留原 TTL）
func (m *MemoryCache) incrBy(key string, delta int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	if e, ok := m.data[key]; ok && !m.expired(e) {
		if err := json.Unmarshal(e.val, &n); err != nil {
			return 0, err
		}
		n += delta
		raw, err := json.Marshal(n)
		if err != nil {
			return 0, err
		}
		e.val = raw
		m.data[key] = e
		return n, nil
	}
	n = delta
	raw, err := json.Marshal(n)
	if err != nil {
		return 0, err
	}
	m.data[key] = memoryEntry{val: raw}
	return n, nil
}

// GetOrSet 读取缓存，未命中时调用 loader 获取值并写入缓存
func (m *MemoryCache) GetOrSet(ctx context.Context, key string, loader func() (any, error), expiration time.Duration) ([]byte, error) {
	if raw, err := m.Get(ctx, key); err == nil {
		return raw, nil
	}
	v, err := loader()
	if err != nil {
		return nil, err
	}
	if err := m.Set(ctx, key, v, expiration); err != nil {
		return nil, err
	}
	return m.Get(ctx, key)
}

// GetDel 原子读取并删除缓存
func (m *MemoryCache) GetDel(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.data[key]
	if !ok || m.expired(e) {
		return nil, ErrKeyNotFound
	}
	delete(m.data, key)
	return e.val, nil
}

// CompareAndDelete atomically deletes key only when its value matches expected.
// A mismatch intentionally preserves the entry, matching the Redis Lua
// implementation used by production verification-code flows.
func (m *MemoryCache) CompareAndDelete(_ context.Context, key string, expected []byte) (bool, error) {
	if m == nil {
		return false, fmt.Errorf("memory cache is unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.data[key]
	if !ok {
		return false, ErrKeyNotFound
	}
	if m.expired(e) {
		delete(m.data, key)
		return false, ErrKeyNotFound
	}
	if subtle.ConstantTimeCompare(e.val, expected) != 1 {
		return false, nil
	}
	delete(m.data, key)
	return true, nil
}

// CompareAndExpire atomically renews a lease only when the current value still
// matches the expected owner token.
func (m *MemoryCache) CompareAndExpire(_ context.Context, key string, expected []byte, expiration time.Duration) (bool, error) {
	if m == nil {
		return false, fmt.Errorf("memory cache is unavailable")
	}
	if expiration <= 0 {
		return false, fmt.Errorf("expiration must be positive")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.data[key]
	if !ok {
		return false, ErrKeyNotFound
	}
	if m.expired(e) {
		delete(m.data, key)
		return false, ErrKeyNotFound
	}
	if subtle.ConstantTimeCompare(e.val, expected) != 1 {
		return false, nil
	}
	e.expiresAt = time.Now().Add(expiration)
	m.data[key] = e
	return true, nil
}

// GetObjectDel 原子读取、反序列化并删除缓存
func (m *MemoryCache) GetObjectDel(ctx context.Context, key string, value any) error {
	raw, err := m.GetDel(ctx, key)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, value)
}

// Flush 清空所有缓存
func (m *MemoryCache) Flush(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = make(map[string]memoryEntry)
	m.zsets = make(map[string]map[string]float64)
	return nil
}

// Close 关闭缓存客户端（内存实现无资源可释放）
func (m *MemoryCache) Close() error {
	return nil
}

// ZAdd 向有序集合添加成员，score 存 Unix 时间戳（滑动窗口场景）。
// 成员已存在时更新 score（与 Redis ZADD 语义一致）。
func (m *MemoryCache) ZAdd(_ context.Context, key string, score float64, member string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.zsets == nil {
		m.zsets = make(map[string]map[string]float64)
	}
	if m.zsets[key] == nil {
		m.zsets[key] = make(map[string]float64)
	}
	m.zsets[key][member] = score
	return nil
}

// ZRangeByScore 返回分数在 [min, max] 区间内的成员，按分数升序（同分按成员字典序，与 Redis 一致）。
// 键不存在或区间无成员时返回空切片（与 Redis 语义一致）。
func (m *MemoryCache) ZRangeByScore(_ context.Context, key, min, max string) ([]string, error) {
	minVal, minInc, err := parseZBound(min)
	if err != nil {
		return nil, err
	}
	maxVal, maxInc, err := parseZBound(max)
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	zs, ok := m.zsets[key]
	if !ok {
		return []string{}, nil
	}

	members := make([]string, 0, len(zs))
	for member, score := range zs {
		if (minInc && score >= minVal || !minInc && score > minVal) &&
			(maxInc && score <= maxVal || !maxInc && score < maxVal) {
			members = append(members, member)
		}
	}

	// 模拟 Redis 排序：分数升序，同分按成员字典序
	sort.Slice(members, func(i, j int) bool {
		si, sj := zs[members[i]], zs[members[j]]
		if si != sj {
			return si < sj
		}
		return members[i] < members[j]
	})
	return members, nil
}

// ZRangeByScoreWithScores 返回分数在 [min, max] 区间内的 (成员, 分数) 对，按分数升序（同分按成员字典序）。
// 键不存在或区间无成员时返回空切片（与 Redis ZRANGEBYSCORE ... WITHSCORES 一致）。
func (m *MemoryCache) ZRangeByScoreWithScores(_ context.Context, key, min, max string) ([]ZSetPair, error) {
	minVal, minInc, err := parseZBound(min)
	if err != nil {
		return nil, err
	}
	maxVal, maxInc, err := parseZBound(max)
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	zs, ok := m.zsets[key]
	if !ok {
		return []ZSetPair{}, nil
	}

	pairs := make([]ZSetPair, 0, len(zs))
	for member, score := range zs {
		if (minInc && score >= minVal || !minInc && score > minVal) &&
			(maxInc && score <= maxVal || !maxInc && score < maxVal) {
			pairs = append(pairs, ZSetPair{Member: member, Score: score})
		}
	}

	// 模拟 Redis 排序：分数升序，同分按成员字典序
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Score != pairs[j].Score {
			return pairs[i].Score < pairs[j].Score
		}
		return pairs[i].Member < pairs[j].Member
	})
	return pairs, nil
}

// ZRemRangeByScore 移除分数在 [min, max] 区间的成员，返回移除数量（与 Redis ZREMRANGEBYSCORE 一致）。
func (m *MemoryCache) ZRemRangeByScore(_ context.Context, key, min, max string) (int64, error) {
	minVal, minInc, err := parseZBound(min)
	if err != nil {
		return 0, err
	}
	maxVal, maxInc, err := parseZBound(max)
	if err != nil {
		return 0, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	zs, ok := m.zsets[key]
	if !ok {
		return 0, nil
	}

	var removed int64
	for member, score := range zs {
		if (minInc && score >= minVal || !minInc && score > minVal) &&
			(maxInc && score <= maxVal || !maxInc && score < maxVal) {
			delete(zs, member)
			removed++
		}
	}
	return removed, nil
}

// ZCard 返回有序集合的成员数量；键不存在时返回 0（与 Redis ZCARD 语义一致）。
func (m *MemoryCache) ZCard(_ context.Context, key string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return int64(len(m.zsets[key])), nil
}

// ZScore 返回指定成员的分数；成员（或键）不存在时返回 ErrKeyNotFound（与 Redis ZSCORE nil 语义对齐）。
func (m *MemoryCache) ZScore(_ context.Context, key, member string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	zs, ok := m.zsets[key]
	if !ok {
		return 0, ErrKeyNotFound
	}
	score, ok := zs[member]
	if !ok {
		return 0, ErrKeyNotFound
	}
	return score, nil
}

// parseZBound 解析 ZRANGEBYSCORE/ZREMRANGEBYSCORE 的边界字符串，返回（数值, 是否闭区间）。
// 支持 Redis 语法："-inf"/"+inf" 无穷边界、"(x" 开区间（x 本身不包含）、纯数字闭区间。
// 字符串传参避免 float64 精度问题（如 "(now-3600" 精确排除窗口边界）。
func parseZBound(s string) (float64, bool, error) {
	switch s {
	case "-inf":
		return math.Inf(-1), true, nil
	case "+inf":
		return math.Inf(1), true, nil
	}
	if strings.HasPrefix(s, "(") {
		v, err := strconv.ParseFloat(strings.TrimPrefix(s, "("), 64)
		if err != nil {
			return 0, false, fmt.Errorf("cache: 非法的 zset 边界 %q", s)
		}
		return v, false, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false, fmt.Errorf("cache: 非法的 zset 边界 %q", s)
	}
	return v, true, nil
}
