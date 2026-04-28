package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisClient struct {
	rdb *redis.Client
	ctx context.Context
}

func initRedis(cfg *Config) *RedisClient {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPass,
		DB:       cfg.RedisDB,
	})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("Redis 连接失败: %v", err)
	}
	log.Println("Redis 连接成功")
	return &RedisClient{rdb: rdb, ctx: ctx}
}

// Close 关闭 Redis 连接
func (rc *RedisClient) Close() error {
	return rc.rdb.Close()
}

// Set 设置字符串，带过期时间
func (rc *RedisClient) Set(key, value string, ttl time.Duration) error {
	return rc.rdb.Set(rc.ctx, key, value, ttl).Err()
}

// Get 获取字符串
func (rc *RedisClient) Get(key string) string {
	v, err := rc.rdb.Get(rc.ctx, key).Result()
	if err == redis.Nil {
		return ""
	}
	if err != nil {
		log.Printf("Redis GET %s 错误: %v", key, err)
		return ""
	}
	return v
}

// Del 删除 key
func (rc *RedisClient) Del(key string) error {
	return rc.rdb.Del(rc.ctx, key).Err()
}

// Exists 判断 key 是否存在
func (rc *RedisClient) Exists(key string) bool {
	n, err := rc.rdb.Exists(rc.ctx, key).Result()
	return err == nil && n > 0
}

// Incr key 自增
func (rc *RedisClient) Incr(key string, delta int64) int64 {
	v, err := rc.rdb.IncrBy(rc.ctx, key, delta).Result()
	if err != nil {
		log.Printf("Redis INCR %s 错误: %v", key, err)
		return 0
	}
	return v
}

// TryLock 尝试获取分布式锁，返回锁 value（释放用），失败返回空串
func (rc *RedisClient) TryLock(key string, timeoutSec int64) string {
	lockKey := "lock:" + key
	lockValue := randomID()
	ok, err := rc.rdb.SetNX(rc.ctx, lockKey, lockValue, time.Duration(timeoutSec)*time.Second).Result()
	if err != nil {
		log.Printf("Redis 加锁 %s 错误: %v", lockKey, err)
		return ""
	}
	if ok {
		return lockValue
	}
	return ""
}

// Unlock 安全释放锁（Lua 原子脚本）
func (rc *RedisClient) Unlock(key, lockValue string) {
	if lockValue == "" {
		return
	}
	lockKey := "lock:" + key
	script := `if redis.call('get', KEYS[1]) == ARGV[1] then return redis.call('del', KEYS[1]) else return 0 end`
	result, err := rc.rdb.Eval(rc.ctx, script, []string{lockKey}, lockValue).Result()
	if err != nil {
		log.Printf("Redis 释放锁 %s 错误: %v", lockKey, err)
		return
	}
	if result.(int64) == 1 {
		log.Printf("释放锁成功: %s", lockKey)
	} else {
		log.Printf("释放锁失败（已过期或被他人持有）: %s", lockKey)
	}
}

// TokenBucketAllow 令牌桶限流，返回 true 表示通过
func (rc *RedisClient) TokenBucketAllow(key string, permitsPerSecond float64, capacity int64) bool {
	now := time.Now().UnixMilli()
	script := `
local key = KEYS[1]
local rate = tonumber(ARGV[1])
local capacity = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

local lastTokens = redis.call('hget', key, 'tokens')
local lastUpdated = redis.call('hget', key, 'updated')

if lastTokens == false then
    lastTokens = capacity
    lastUpdated = now
else
    lastTokens = tonumber(lastTokens)
    lastUpdated = tonumber(lastUpdated)
end

local elapsed = (now - lastUpdated) / 1000.0
local newTokens = math.min(capacity, lastTokens + elapsed * rate)

if newTokens >= requested then
    newTokens = newTokens - requested
    redis.call('hset', key, 'tokens', newTokens)
    redis.call('hset', key, 'updated', now)
    redis.call('expire', key, 600)
    return 1
else
    redis.call('hset', key, 'tokens', newTokens)
    redis.call('hset', key, 'updated', now)
    redis.call('expire', key, 600)
    return 0
end
`
	result, err := rc.rdb.Eval(rc.ctx, script, []string{key},
		fmt.Sprintf("%f", permitsPerSecond),
		fmt.Sprintf("%d", capacity),
		fmt.Sprintf("%d", now),
		"1",
	).Result()
	if err != nil {
		log.Printf("限流脚本错误: %v", err)
		return false
	}
	return result.(int64) == 1
}
