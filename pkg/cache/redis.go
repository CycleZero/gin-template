package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"gin-template/pkg/trace"

	"github.com/redis/go-redis/v9"
)

// RedisCache 基于 Redis 的缓存实现
type RedisCache struct {
	client *redis.Client
}

// Client 仅供需要 Redis 原子脚本的基础设施适配器使用；领域层不得依赖它。
func (r *RedisCache) Client() *redis.Client { return r.client }

// NewRedisCache 创建 RedisCache 实例并验证连接。
//
// 与 pkg/infra.NewRedisClient 一致，在此集中挂载 OTel 追踪 Hook，
// 使缓存读写自动产生 span 与耗时指标，业务侧无感。
func NewRedisCache(host string, port int, password string, db int) Cache {
	return NewRedisCacheWithCredentials(host, port, "", password, db)
}

// NewRedisCacheWithCredentials 用于独立配置的共享缓存连接，保留统一追踪和连接策略。
func NewRedisCacheWithCredentials(host string, port int, username, password string, db int) Cache {
	addr := host + ":" + strconv.Itoa(port)
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Username:     username,
		Password:     password,
		DB:           db,
		PoolSize:     20, // 限制连接池大小（默认 10×CPU=80，5 个服务共 400 连接过度浪费）
		MinIdleConns: 5,  // 保持少量热连接，减少冷启动延迟
	})
	client.AddHook(trace.NewRedisHook())

	// 启动时 PING 验证连接
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		fmt.Printf("[cache] Redis 连接失败 %s (db=%d): %v\n", addr, db, err)
	} else {
		fmt.Printf("[cache] Redis 连接成功 %s (db=%d)\n", addr, db)
	}

	return &RedisCache{client: client}
}

// NewRedisCacheWithClient 使用已有的 redis.Client 创建 RedisCache
func NewRedisCacheWithClient(client *redis.Client) *RedisCache {
	return &RedisCache{client: client}
}

// Get 获取缓存值
func (r *RedisCache) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrKeyNotFound
		}
		return nil, err
	}
	return []byte(val), nil
}

// GetObject 获取缓存并反序列化为对象，value 必须为指针
func (r *RedisCache) GetObject(ctx context.Context, key string, value any) error {
	val, err := r.Get(ctx, key)
	if err != nil {
		return err
	}
	return json.Unmarshal(val, value)
}

// MGet 批量获取缓存，返回 key → 原始字节映射。未命中或错误的 key 不出现在结果中。
func (r *RedisCache) MGet(ctx context.Context, keys []string) (map[string][]byte, error) {
	if len(keys) == 0 {
		return make(map[string][]byte), nil
	}
	vals, err := r.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	result := make(map[string][]byte, len(keys))
	for i, v := range vals {
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok {
			result[keys[i]] = []byte(s)
		}
	}
	return result, nil
}

// Set 设置缓存，expiration=0 表示永不过期
func (r *RedisCache) Set(ctx context.Context, key string, value any, expiration time.Duration) error {
	var data []byte
	var err error

	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		data, err = json.Marshal(v)
		if err != nil {
			return err
		}
	}

	if expiration == 0 {
		return r.client.Set(ctx, key, data, 0).Err()
	}
	return r.client.Set(ctx, key, data, expiration).Err()
}

// Delete 删除缓存
func (r *RedisCache) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

// Exists 判断键是否存在
func (r *RedisCache) Exists(ctx context.Context, key string) (bool, error) {
	n, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// TTL 获取键剩余过期时间
// 返回 -2 表示键不存在，返回 -1 表示永不过期
func (r *RedisCache) TTL(ctx context.Context, key string) (time.Duration, error) {
	return r.client.TTL(ctx, key).Result()
}

// Flush 清空当前数据库的所有缓存
func (r *RedisCache) Flush(ctx context.Context) error {
	return r.client.FlushDB(ctx).Err()
}

// Close 关闭 Redis 客户端连接
func (r *RedisCache) Close() error {
	return r.client.Close()
}

// SetNX 仅当键不存在时设置缓存，返回 true 表示设置成功。
func (r *RedisCache) SetNX(ctx context.Context, key string, value any, expiration time.Duration) (bool, error) {
	var data []byte
	var err error

	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		data, err = json.Marshal(v)
		if err != nil {
			return false, err
		}
	}

	return r.client.SetNX(ctx, key, data, expiration).Result()
}

// Expire 设置键的过期时间，不修改值。
func (r *RedisCache) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return r.client.Expire(ctx, key, expiration).Err()
}

// Incr 原子递增 1，返回递增后的值。
func (r *RedisCache) Incr(ctx context.Context, key string) (int64, error) {
	return r.client.Incr(ctx, key).Result()
}

// Decr 原子递减 1，返回递减后的值。
func (r *RedisCache) Decr(ctx context.Context, key string) (int64, error) {
	return r.client.Decr(ctx, key).Result()
}

// GetOrSet 读取缓存，未命中时调用 loader 获取值并写入缓存，返回最终值。
func (r *RedisCache) GetOrSet(ctx context.Context, key string, loader func() (any, error), expiration time.Duration) ([]byte, error) {
	val, err := r.Get(ctx, key)
	if err == nil {
		return val, nil
	}
	if !errors.Is(err, ErrKeyNotFound) {
		return nil, err
	}

	loaded, err := loader()
	if err != nil {
		return nil, err
	}

	if setErr := r.Set(ctx, key, loaded, expiration); setErr != nil {
		return nil, setErr
	}

	// 读取刚写入的值，确保返回格式一致
	return r.Get(ctx, key)
}

// GetDel 调用 go-redis 内置 GETDEL 命令，原子读取并删除缓存（Redis 6.2.0+）
func (r *RedisCache) GetDel(ctx context.Context, key string) ([]byte, error) {
	val, err := r.client.GetDel(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrKeyNotFound
		}
		return nil, err
	}
	return []byte(val), nil
}

// CompareAndDelete compares a value and deletes the key in one Redis command.
//
// The method deliberately is not part of Cache: only security-sensitive callers
// that require this stronger primitive should opt in through a narrow interface.
// A mismatch leaves the key untouched so a mistyped verification code cannot
// consume the valid one.
func (r *RedisCache) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	if r == nil || r.client == nil {
		return false, errors.New("redis cache is unavailable")
	}

	const compareAndDeleteScript = `
local value = redis.call('GET', KEYS[1])
if not value then
  return -1
end
if value ~= ARGV[1] then
  return 0
end
redis.call('DEL', KEYS[1])
return 1
`
	result, err := r.client.Eval(ctx, compareAndDeleteScript, []string{key}, expected).Int64()
	if err != nil {
		return false, err
	}
	switch result {
	case -1:
		return false, ErrKeyNotFound
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("unexpected compare-and-delete result: %d", result)
	}
}

// CompareAndExpire renews a lease only while the stored owner token still
// matches. It is intentionally exposed as a narrow opt-in primitive (like
// CompareAndDelete) so lock users cannot perform a GET/EXPIRE race that might
// extend a lease acquired by another owner after expiry.
func (r *RedisCache) CompareAndExpire(ctx context.Context, key string, expected []byte, expiration time.Duration) (bool, error) {
	if r == nil || r.client == nil {
		return false, errors.New("redis cache is unavailable")
	}
	if expiration <= 0 {
		return false, errors.New("expiration must be positive")
	}
	const compareAndExpireScript = `
local value = redis.call('GET', KEYS[1])
if not value then
  return -1
end
if value ~= ARGV[1] then
  return 0
end
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return 1
`
	result, err := r.client.Eval(ctx, compareAndExpireScript, []string{key}, expected, expiration.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	switch result {
	case -1:
		return false, ErrKeyNotFound
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("unexpected compare-and-expire result: %d", result)
	}
}

// GetObjectDel 原子读取、反序列化并删除缓存，value 必须为指针
func (r *RedisCache) GetObjectDel(ctx context.Context, key string, value any) error {
	val, err := r.GetDel(ctx, key)
	if err != nil {
		return err
	}
	return json.Unmarshal(val, value)
}

// ZAdd 向有序集合添加成员（Redis ZADD），score 存 Unix 时间戳（滑动窗口场景）。
// 对齐 Laravel TailService::recordCustomStyleChange() 的 `Redis::zadd($key, $now, $member)`（TailService.php:1705）。
func (r *RedisCache) ZAdd(ctx context.Context, key string, score float64, member string) error {
	return r.client.ZAdd(ctx, key, redis.Z{Score: score, Member: member}).Err()
}

// ZRangeByScore 按分数升序返回 [min, max] 区间成员（Redis ZRANGEBYSCORE）。
// min/max 支持 "-inf"/"+inf"/"(x" 开区间语法，与 Redis 一致；键不存在时返回空切片。
// 对齐 Laravel 的 `Redis::zrange($key, 0, 0)` 取最旧一次变更（TailService.php:1548）。
func (r *RedisCache) ZRangeByScore(ctx context.Context, key, min, max string) ([]string, error) {
	return r.client.ZRangeByScore(ctx, key, &redis.ZRangeBy{Min: min, Max: max}).Result()
}

// ZRangeByScoreWithScores 按分数升序返回 [min, max] 区间的 (成员, 分数) 对（Redis ZRANGEBYSCORE ... WITHSCORES）。
// 一次性取回成员与分数，避免逐成员 ZScore（行为序列 tid+时间戳批量读）。键不存在时返回空切片。
func (r *RedisCache) ZRangeByScoreWithScores(ctx context.Context, key, min, max string) ([]ZSetPair, error) {
	zs, err := r.client.ZRangeByScoreWithScores(ctx, key, &redis.ZRangeBy{Min: min, Max: max}).Result()
	if err != nil {
		return nil, err
	}
	out := make([]ZSetPair, len(zs))
	for i, z := range zs {
		member, _ := z.Member.(string)
		out[i] = ZSetPair{Member: member, Score: z.Score}
	}
	return out, nil
}

// ZRemRangeByScore 移除分数在 [min, max] 区间的成员，返回移除数量（Redis ZREMRANGEBYSCORE）。
// 对齐 Laravel 滑动窗口清理 `Redis::zremrangebyscore($key, '-inf', $now - 3600)`（TailService.php:1546）。
func (r *RedisCache) ZRemRangeByScore(ctx context.Context, key, min, max string) (int64, error) {
	return r.client.ZRemRangeByScore(ctx, key, min, max).Result()
}

// ZCard 返回有序集合成员数量（Redis ZCARD）；键不存在时 Redis 返回 0。
// 对齐 Laravel 的 `Redis::zcard($key)` 统计窗口内使用次数（TailService.php:1547）。
func (r *RedisCache) ZCard(ctx context.Context, key string) (int64, error) {
	return r.client.ZCard(ctx, key).Result()
}

// ZScore 返回成员分数（Redis ZSCORE）；成员或键不存在时（redis.Nil）返回 ErrKeyNotFound。
// 对齐 Laravel 的 `Redis::zscore($key, $member)` 取最旧一次的分数以计算重置时间（TailService.php:1550）。
func (r *RedisCache) ZScore(ctx context.Context, key, member string) (float64, error) {
	score, err := r.client.ZScore(ctx, key, member).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, ErrKeyNotFound
		}
		return 0, err
	}
	return score, nil
}
