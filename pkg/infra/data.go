package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gin-template/internal/conf"
	"gin-template/pkg/log"
	"gin-template/pkg/trace"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 连接池参数：默认值在长连接场景下过于保守/激进，这里给出显式取值。
const (
	maxOpenConns    = 50               // 最大连接数（按 DB 承载能力调整）
	maxIdleConns    = 10               // 空闲连接数
	connMaxLifetime = 30 * time.Minute // 短于 MySQL wait_timeout，避免用到已被服务端关闭的连接
	connMaxIdleTime = 10 * time.Minute
)

type Data struct {
	DB          *gorm.DB
	RedisClient *RedisClient
}

type RedisClient struct {
	*redis.Client
}

// GetObject 从 Redis 获取并反序列化为目标对象
func (r *RedisClient) GetObject(ctx context.Context, key string, target any) error {
	res, err := r.Get(ctx, key).Result()
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(res), target)
}

// PutObject 序列化对象并存入 Redis
func (r *RedisClient) PutObject(ctx context.Context, key string, target any, expiration time.Duration) error {
	str, err := json.Marshal(target)
	if err != nil {
		return err
	}
	return r.SetEx(ctx, key, string(str), expiration).Err()
}

func NewData(cfg *conf.Bootstrap, rdb *RedisClient) *Data {
	masterDB, err := gorm.Open(mysql.Open(cfg.GetData().GetDb().DSN()), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		log.Fatal("连接数据库失败", "error", err)
	}

	if sqlDB, err := masterDB.DB(); err == nil {
		sqlDB.SetMaxOpenConns(maxOpenConns)
		sqlDB.SetMaxIdleConns(maxIdleConns)
		sqlDB.SetConnMaxLifetime(connMaxLifetime)
		sqlDB.SetConnMaxIdleTime(connMaxIdleTime)
	}

	return &Data{
		DB:          masterDB,
		RedisClient: rdb,
	}
}

func NewRedisClient(cfg *conf.Bootstrap) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.GetData().GetRedis().Addr(),
		Password: cfg.GetData().GetRedis().GetPassword(),
		DB:       0,
	})
	// Redis 命令级 span 与耗时指标：以 Hook 方式挂载，不改动任何调用方代码；
	// OTel Provider 未启用时开销趋近于零。
	return trace.HookRedis(rdb)
}

func NewCustomRedisClient(rdb *redis.Client) *RedisClient {
	return &RedisClient{rdb}
}

// Health 探测依赖可用性，供 GET /readyz 使用。
//
// 逐个探测 MySQL 与 Redis，把不可用的依赖名拼进错误信息（只进日志，不下发客户端）。
// 未初始化的依赖（如未配置 Redis）会被跳过。
func (d *Data) Health(ctx context.Context) error {
	if d == nil {
		return errors.New("infra: Data 未初始化")
	}

	var problems []error
	if d.DB != nil {
		if sqlDB, err := d.DB.DB(); err != nil {
			problems = append(problems, fmt.Errorf("mysql: %w", err))
		} else if err := sqlDB.PingContext(ctx); err != nil {
			problems = append(problems, fmt.Errorf("mysql: %w", err))
		}
	}
	if d.RedisClient != nil {
		if err := d.RedisClient.Ping(ctx).Err(); err != nil {
			problems = append(problems, fmt.Errorf("redis: %w", err))
		}
	}
	return errors.Join(problems...)
}

// Close 释放数据库与 Redis 连接，进程退出（优雅停机）时调用。
//
// 用 errors.Join 汇总所有关闭错误：即使其中一项失败，也要继续关掉其余的，
// 否则会泄漏连接。
func (d *Data) Close() error {
	if d == nil {
		return nil
	}

	var problems []error
	if d.DB != nil {
		if sqlDB, err := d.DB.DB(); err != nil {
			problems = append(problems, fmt.Errorf("mysql: %w", err))
		} else if err := sqlDB.Close(); err != nil {
			problems = append(problems, fmt.Errorf("mysql: %w", err))
		}
	}
	if d.RedisClient != nil {
		if err := d.RedisClient.Close(); err != nil {
			problems = append(problems, fmt.Errorf("redis: %w", err))
		}
	}
	return errors.Join(problems...)
}

// GetDB 从 Data 中获取 *gorm.DB 供 Wire 注入
func GetDB(data *Data) *gorm.DB {
	return data.DB
}
