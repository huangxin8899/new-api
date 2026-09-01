package common

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// 渐进式人机验证的“可疑度”计数器。
//
// 无条件挂载 Turnstile 会让每个访客一进登录/注册页就去加载
// challenges.cloudflare.com，该域名在部分地区延迟极高甚至超时，首屏因此
// 被拖慢。改为按客户端 IP 累计可疑行为（登录失败、注册、发验证邮件等），
// 只有越过阈值的 IP 才被要求过人机验证；正常用户一次都碰不到 Turnstile。

var (
	// ChallengeTriggerThreshold 是触发人机验证所需的窗口内可疑次数。
	// 设为 0 表示不做渐进，任何请求都要求人机验证（等同于旧的无条件行为）。
	ChallengeTriggerThreshold = 2
	// ChallengeWindowSeconds 是计数窗口，窗口内没有新的可疑行为则计数自动归零。
	ChallengeWindowSeconds int64 = 60 * 60
)

// ChallengeScopeAuth 覆盖登录、注册、发验证码、发重置邮件——它们共享同一份
// 可疑度，所以在登录接口上刷密码的 IP 换去刷注册接口时同样会被拦下。
const ChallengeScopeAuth = "auth"

const challengeKeyNamespace = "challenge:v1"

// challengeIncrScript 让 INCR 与首次 EXPIRE 原子化，避免计数器丢掉 TTL 变成永久键。
const challengeIncrScript = `
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return count
`

func challengeKey(scope string, ip string) string {
	return fmt.Sprintf("%s:%s:%s", challengeKeyNamespace, scope, ip)
}

func challengeRedisReady() bool {
	return RedisEnabled && RDB != nil
}

type challengeEntry struct {
	count     int64
	expiresAt time.Time
}

// challengeMemoryStore 是未启用 Redis 时的单机兜底。多实例部署下每个实例各记
// 各的，阈值实际被放大到 实例数 × 阈值——要精确控制就必须开 Redis。
type challengeMemoryStore struct {
	mutex     sync.Mutex
	entries   map[string]challengeEntry
	lastSweep time.Time
}

var challengeMemory = &challengeMemoryStore{entries: make(map[string]challengeEntry)}

func (s *challengeMemoryStore) incr(key string, ttl time.Duration, now time.Time) int64 {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.sweepLocked(now)
	entry, ok := s.entries[key]
	if !ok || now.After(entry.expiresAt) {
		entry = challengeEntry{expiresAt: now.Add(ttl)}
	}
	entry.count++
	s.entries[key] = entry
	return entry.count
}

func (s *challengeMemoryStore) get(key string, now time.Time) int64 {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	entry, ok := s.entries[key]
	if !ok || now.After(entry.expiresAt) {
		return 0
	}
	return entry.count
}

func (s *challengeMemoryStore) del(key string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.entries, key)
}

// sweepLocked 惰性回收过期条目；调用方必须持有锁。每分钟最多扫一次，
// 避免登录高峰时每次请求都遍历整张表。
func (s *challengeMemoryStore) sweepLocked(now time.Time) {
	if now.Sub(s.lastSweep) < time.Minute {
		return
	}
	s.lastSweep = now
	for key, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, key)
		}
	}
}

// RecordChallengeStrike 记录该 IP 的一次可疑行为，返回窗口内的累计次数。
func RecordChallengeStrike(ctx context.Context, scope string, ip string) int64 {
	if ip == "" {
		return 0
	}
	key := challengeKey(scope, ip)
	if challengeRedisReady() {
		count, err := RDB.Eval(ctx, challengeIncrScript, []string{key}, ChallengeWindowSeconds).Int64()
		if err == nil {
			return count
		}
		SysError("challenge strike incr failed, falling back to memory: " + err.Error())
	}
	return challengeMemory.incr(key, time.Duration(ChallengeWindowSeconds)*time.Second, time.Now())
}

// ChallengeStrikes 读取窗口内累计的可疑次数，不改变计数。
func ChallengeStrikes(ctx context.Context, scope string, ip string) int64 {
	if ip == "" {
		return 0
	}
	key := challengeKey(scope, ip)
	if challengeRedisReady() {
		count, err := RDB.Get(ctx, key).Int64()
		if err == nil {
			return count
		}
		if errors.Is(err, redis.Nil) {
			return 0
		}
		SysError("challenge strike read failed, falling back to memory: " + err.Error())
	}
	return challengeMemory.get(key, time.Now())
}

// ClearChallengeStrikes 在确认对方是真人后（例如登录成功）清零计数，
// 避免同一出口 IP 后面的用户被前面的失败连累。
func ClearChallengeStrikes(ctx context.Context, scope string, ip string) {
	if ip == "" {
		return
	}
	key := challengeKey(scope, ip)
	if challengeRedisReady() {
		if err := RDB.Del(ctx, key).Err(); err != nil && !errors.Is(err, redis.Nil) {
			SysError("challenge strike clear failed: " + err.Error())
		}
	}
	challengeMemory.del(key)
}

// ChallengeRequired 判断该 IP 是否已越过阈值、本次请求必须过人机验证。
func ChallengeRequired(ctx context.Context, scope string, ip string) bool {
	if ChallengeTriggerThreshold <= 0 {
		return true
	}
	return ChallengeStrikes(ctx, scope, ip) >= int64(ChallengeTriggerThreshold)
}
