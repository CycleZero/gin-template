package cache

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"
)

// 测试滑动窗口语义对齐 Laravel TailService::getCustomStyleQuota()（TailService.php:1540-1572）：
// zremrangebyscore(-inf, now-3600) + zcard + zrange/zscore。
// MemoryCache 无 Redis 依赖，可在无环境单测；RedisCache 由编译期断言保证接口实现一致。
func TestMemoryCacheZSet(t *testing.T) {
	ctx := context.Background()
	mc := NewMemoryCache()

	// 场景 1：ZAdd 3 个成员后 ZCard=3
	now := float64(time.Now().Unix())
	if err := mc.ZAdd(ctx, "q:1", now-3000, "m1"); err != nil {
		t.Fatalf("ZAdd m1 失败: %v", err)
	}
	if err := mc.ZAdd(ctx, "q:1", now-2000, "m2"); err != nil {
		t.Fatalf("ZAdd m2 失败: %v", err)
	}
	if err := mc.ZAdd(ctx, "q:1", now-1000, "m3"); err != nil {
		t.Fatalf("ZAdd m3 失败: %v", err)
	}
	if n, err := mc.ZCard(ctx, "q:1"); err != nil || n != 3 {
		t.Fatalf("ZCard 期望 3, 得到 %d, err=%v", n, err)
	}

	// 场景 2：ZRangeByScore 按分数升序返回（对齐 Laravel zrange 取最旧一次）
	members, err := mc.ZRangeByScore(ctx, "q:1", "-inf", "+inf")
	if err != nil {
		t.Fatalf("ZRangeByScore 失败: %v", err)
	}
	want := []string{"m1", "m2", "m3"}
	if !reflect.DeepEqual(members, want) {
		t.Fatalf("ZRangeByScore 期望 %v, 得到 %v", want, members)
	}

	// 开区间边界："(now-2000"（数字字符串）排除恰好等于边界的成员
	exclusive, err := mc.ZRangeByScore(ctx, "q:1", "("+formatScore(now-2000), "+inf")
	if err != nil {
		t.Fatalf("ZRangeByScore 开区间失败: %v", err)
	}
	if !reflect.DeepEqual(exclusive, []string{"m3"}) {
		t.Fatalf("开区间期望 [m3], 得到 %v", exclusive)
	}

	// 场景 3：空 zset（键不存在）ZCard=0，ZRangeByScore 返回空切片不报错
	if n, err := mc.ZCard(ctx, "q:missing"); err != nil || n != 0 {
		t.Fatalf("空 zset ZCard 期望 0, 得到 %d, err=%v", n, err)
	}
	empty, err := mc.ZRangeByScore(ctx, "q:missing", "-inf", "+inf")
	if err != nil || len(empty) != 0 {
		t.Fatalf("空 zset ZRangeByScore 期望空切片, 得到 %v, err=%v", empty, err)
	}

	// 场景 4：ZScore 不存在成员返回 ErrKeyNotFound
	if _, err := mc.ZScore(ctx, "q:1", "ghost"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("ZScore 不存在成员期望 ErrKeyNotFound, 得到 %v", err)
	}
	if _, err := mc.ZScore(ctx, "q:missing", "m1"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("ZScore 不存在键期望 ErrKeyNotFound, 得到 %v", err)
	}
	if s, err := mc.ZScore(ctx, "q:1", "m1"); err != nil || s != now-3000 {
		t.Fatalf("ZScore m1 期望 %v, 得到 %v, err=%v", now-3000, s, err)
	}

	// 场景 5：ZRemRangeByScore 移除过期窗口（对齐 Laravel zremrangebyscore(key,'-inf',now-3600)）
	// 构造一个过期成员 + 一个窗口内成员
	if err := mc.ZAdd(ctx, "q:2", now-7200, "old"); err != nil {
		t.Fatalf("ZAdd old 失败: %v", err)
	}
	if err := mc.ZAdd(ctx, "q:2", now-600, "fresh"); err != nil {
		t.Fatalf("ZAdd fresh 失败: %v", err)
	}
	windowMin := now - 3600
	removed, err := mc.ZRemRangeByScore(ctx, "q:2", "-inf", formatScore(windowMin))
	if err != nil {
		t.Fatalf("ZRemRangeByScore 失败: %v", err)
	}
	if removed != 1 {
		t.Fatalf("移除过期窗口期望 1 个, 得到 %d", removed)
	}
	if n, err := mc.ZCard(ctx, "q:2"); err != nil || n != 1 {
		t.Fatalf("移除后 ZCard 期望 1, 得到 %d, err=%v", n, err)
	}
	left, err := mc.ZRangeByScore(ctx, "q:2", "-inf", "+inf")
	if err != nil || !reflect.DeepEqual(left, []string{"fresh"}) {
		t.Fatalf("移除后剩余期望 [fresh], 得到 %v, err=%v", left, err)
	}

	// ZAdd 同分排序（模拟 Redis 同分按字典序）：同分时字典序小的在前
	if err := mc.ZAdd(ctx, "q:3", 100, "b"); err != nil {
		t.Fatalf("ZAdd b 失败: %v", err)
	}
	if err := mc.ZAdd(ctx, "q:3", 100, "a"); err != nil {
		t.Fatalf("ZAdd a 失败: %v", err)
	}
	ties, err := mc.ZRangeByScore(ctx, "q:3", "-inf", "+inf")
	if err != nil || !reflect.DeepEqual(ties, []string{"a", "b"}) {
		t.Fatalf("同分排序期望 [a b], 得到 %v, err=%v", ties, err)
	}

	// ZAdd 覆盖已有成员分数
	if err := mc.ZAdd(ctx, "q:3", 200, "a"); err != nil {
		t.Fatalf("ZAdd 覆盖失败: %v", err)
	}
	if s, err := mc.ZScore(ctx, "q:3", "a"); err != nil || s != 200 {
		t.Fatalf("覆盖后 ZScore 期望 200, 得到 %v, err=%v", s, err)
	}
}

// formatScore 输出窗口边界字符串（整数形式，避免浮点误差）
func formatScore(f float64) string {
	return strconv.FormatFloat(f, 'f', 0, 64)
}

// TestMemoryCacheZRangeByScoreWithScores 新方法：一次取回 (成员, 分数) 对，按分数升序 → 供行为序列读 tid+时间戳。
func TestMemoryCacheZRangeByScoreWithScores(t *testing.T) {
	ctx := context.Background()
	mc := NewMemoryCache()
	now := float64(time.Now().Unix())

	if err := mc.ZAdd(ctx, "beh:a", now-3000, "101"); err != nil {
		t.Fatalf("ZAdd 101 失败: %v", err)
	}
	if err := mc.ZAdd(ctx, "beh:a", now-1000, "103"); err != nil {
		t.Fatalf("ZAdd 103 失败: %v", err)
	}
	if err := mc.ZAdd(ctx, "beh:a", now-2000, "102"); err != nil {
		t.Fatalf("ZAdd 102 失败: %v", err)
	}

	// 按分数升序返回 (member, score)
	pairs, err := mc.ZRangeByScoreWithScores(ctx, "beh:a", "-inf", "+inf")
	if err != nil {
		t.Fatalf("ZRangeByScoreWithScores 失败: %v", err)
	}
	want := []ZSetPair{
		{Member: "101", Score: now - 3000},
		{Member: "102", Score: now - 2000},
		{Member: "103", Score: now - 1000},
	}
	if !reflect.DeepEqual(pairs, want) {
		t.Fatalf("ZRangeByScoreWithScores 期望 %v, 得到 %v", want, pairs)
	}

	// 空 zset 返回空切片不报错
	empty, err := mc.ZRangeByScoreWithScores(ctx, "beh:missing", "-inf", "+inf")
	if err != nil || len(empty) != 0 {
		t.Fatalf("空 zset 期望空切片, 得到 %v, err=%v", empty, err)
	}
}
