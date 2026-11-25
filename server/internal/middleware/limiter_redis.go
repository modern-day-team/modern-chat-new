package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLimiter 固定窗口限流（按分钟计数），用于多实例共享配额。
//
// 语义：每个 key 每分钟最多 perMin 次；窗口边界可能有最多一个窗口的突发（固定窗口的固有特性）。
// 故障策略：Redis 出错时一律放行（fail-open）——限流是保护手段，不应成为单点故障。
type RedisLimiter struct {
	client *redis.Client
	perMin int
	prefix string
}

// NewRedisLimiter 创建共享配额限流器。namespace 用于区分用途：不同接口即使
// 限流同一个 IP/UID，也必须落在不同键前缀，否则会互相挤占配额（例如登录失败
// 5 次后迁移接口也被连带限流）。不传 namespace 时兼容旧调用的默认前缀。
func NewRedisLimiter(client *redis.Client, perMin int, namespace ...string) *RedisLimiter {
	prefix := "mc:rl"
	if len(namespace) > 0 && namespace[0] != "" {
		prefix = "mc:rl:" + namespace[0]
	}
	return &RedisLimiter{client: client, perMin: perMin, prefix: prefix}
}

// LimiterFactory 按用途创建限流器；name 作为 Redis 键前缀隔离不同接口。
type LimiterFactory func(name string, perMin, burst int) Limiter

// MemoryLimiterFactory 内存版工厂（单实例或 Redis 未启用时回退）。
func MemoryLimiterFactory(_ string, perMin, burst int) Limiter {
	return NewLimiterStore(perMin, burst)
}

// RedisLimiterFactory Redis 版工厂（多实例共享配额）。
func RedisLimiterFactory(client *redis.Client) LimiterFactory {
	return func(name string, perMin, _ int) Limiter {
		return NewRedisLimiter(client, perMin, name)
	}
}

// key 构造 Redis 计数键：用途前缀 + 调用方 key + 分钟桶。
func (l *RedisLimiter) key(callerKey string) string {
	return fmt.Sprintf("%s:%s:%d", l.prefix, callerKey, time.Now().Unix()/60)
}

// incrWithTTL 原子地 INCR 并在首次访问时设置 TTL。
// 历史实现是 Incr + 条件 Expire 两次往返：若进程在两次调用之间崩溃，键将永不过期，
// 攻击者可用随机 key 无限堆积内存。Lua 脚本把两步合为一次原子操作消除该窗口。
var incrWithTTL = redis.NewScript(`
local n = redis.call("INCR", KEYS[1])
if n == 1 then
  redis.call("EXPIRE", KEYS[1], ARGV[1])
end
return n
`)

const limiterKeyTTL = 70 // 秒，略大于 60s 窗口，避免边界请求被提前清除

func (l *RedisLimiter) Allow(key string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	k := l.key(key)
	n, err := incrWithTTL.Run(ctx, l.client, []string{k}, limiterKeyTTL).Int64()
	if err != nil {
		slog.Warn("redis limiter unavailable, allowing request", "err", err.Error())
		return true
	}
	return n <= int64(l.perMin)
}
