package infra

import (
	"context"
	"encoding/json"
	"time"

	"gin-template/conf"
	"gin-template/pkg/log"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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

func NewData(cfg *conf.Config, rdb *RedisClient) *Data {
	masterDB, err := gorm.Open(mysql.Open(cfg.Data.DB.DSN()), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		log.Fatal("连接数据库失败", "error", err)
	}

	return &Data{
		DB:          masterDB,
		RedisClient: rdb,
	}
}

func NewRedisClient(cfg *conf.Config) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Data.Redis.Addr(),
		Password: cfg.Data.Redis.Password,
		DB:       0,
	})
	return rdb
}

func NewCustomRedisClient(rdb *redis.Client) *RedisClient {
	return &RedisClient{rdb}
}

// GetDB 从 Data 中获取 *gorm.DB 供 Wire 注入
func GetDB(data *Data) *gorm.DB {
	return data.DB
}
