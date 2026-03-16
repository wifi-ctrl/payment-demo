package cache

import (
	"context"
	"log"
	"time"
)

// Redis 封装 Redis 连接（demo 用接口模拟，生产环境用 go-redis）
type Redis struct {
	// 生产环境: rdb *redis.Client
}

func NewRedis(addr string) *Redis {
	log.Printf("[Infra] Redis connected to %s", addr)
	return &Redis{}
}

// Get 从缓存获取
func (r *Redis) Get(ctx context.Context, key string) ([]byte, error) {
	// 生产环境: return r.rdb.Get(ctx, key).Bytes()
	return nil, ErrCacheMiss
}

// Set 写入缓存
func (r *Redis) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	// 生产环境: return r.rdb.Set(ctx, key, value, ttl).Err()
	return nil
}

// Delete 删除缓存
func (r *Redis) Delete(ctx context.Context, key string) error {
	// 生产环境: return r.rdb.Del(ctx, key).Err()
	return nil
}
