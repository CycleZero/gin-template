package cache

import "context"

// ZSetCache 可选的有序集合（Sorted Set）缓存接口。
// 注意：这是独立于 Cache 主接口的可选扩展接口，实现者必须同时实现 Cache。
// 调用方通过类型断言探测能力：`if zc, ok := c.(cache.ZSetCache); ok { ... }`
// 保持 Cache 主接口不动，避免破坏 8+ 个共享服务（comment/thread/recommend/notification/pay/component/user/decoration）的回归面。
//
// 对齐 Laravel TailService::getCustomStyleQuota()（TailService.php:1540-1572）与
// TailService::recordCustomStyleChange()（TailService.php:1697-1707）的滑动窗口语义：
//   - zremrangebyscore(key, '-inf', now-3600)  → ZRemRangeByScore 移除过期窗口
//   - zcard(key)                               → ZCard 统计窗口内次数
//   - zrange(key, 0, 0) + zscore(key, member)  → ZRangeByScore/ZScore 取最旧一次以计算重置时间
//   - zadd(key, now, member)                   → ZAdd 记录一次变更
//
// 其中 score 存 Unix 时间戳，member 存唯一标识（如变更序号）。
// ZSetPair 有序集合的「成员 + 分数」对（WITHSCORES 语义）。
// 用于需要同时取成员与分数（如行为序列的 tid+时间戳）的批量场景。
type ZSetPair struct {
	Member string
	Score  float64
}

type ZSetCache interface {
	// ZAdd 向有序集合添加成员，score 为该成员分数（滑动窗口场景存 Unix 时间戳）。
	// 成员已存在时更新其 score（与 Redis ZADD 语义一致）。
	ZAdd(ctx context.Context, key string, score float64, member string) error
	// ZRangeByScore 返回分数在 [min, max] 区间内的成员，按分数升序（同分按成员字典序）。
	// min/max 使用字符串：支持 "-inf"/"+inf" 无穷边界，以及 "(x" 表示开区间（与 Redis ZRANGEBYSCORE 一致），
	// 避免 float64 精度问题（如 "(now-3600" 精确排除窗口边界）。
	// 键不存在或区间无成员时返回空切片，不报错（与 Redis 语义一致）。
	ZRangeByScore(ctx context.Context, key, min, max string) ([]string, error)
	// ZRangeByScoreWithScores 返回分数在 [min, max] 区间内的 (成员, 分数) 对，按分数升序（同分按成员字典序）。
	// 语义同 ZRangeByScore，但附带分数（WITHSCORES）——用于行为序列一次取回 tid+时间戳，避免逐成员 ZScore。
	// 键不存在或区间无成员时返回空切片，不报错（与 Redis 语义一致）。
	ZRangeByScoreWithScores(ctx context.Context, key, min, max string) ([]ZSetPair, error)
	// ZRemRangeByScore 移除分数在 [min, max] 区间内的成员，返回移除数量。
	// 用于滑动窗口清理过期记录：ZRemRangeByScore(key, "-inf", 窗口左界)。
	ZRemRangeByScore(ctx context.Context, key, min, max string) (int64, error)
	// ZCard 返回有序集合的成员数量；键不存在时返回 0（与 Redis ZCARD 语义一致）。
	ZCard(ctx context.Context, key string) (int64, error)
	// ZScore 返回指定成员的分数；成员（或键）不存在时返回 ErrKeyNotFound。
	ZScore(ctx context.Context, key, member string) (float64, error)
}

// 编译期断言：两个缓存实现都必须满足 ZSetCache 接口
var (
	_ ZSetCache = (*RedisCache)(nil)
	_ ZSetCache = (*MemoryCache)(nil)
)
