package common

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetChallengeMemory 隔离测试用的内存计数器，避免用例之间互相污染。
func resetChallengeMemory(t *testing.T) {
	t.Helper()
	previous := challengeMemory
	challengeMemory = &challengeMemoryStore{entries: make(map[string]challengeEntry)}
	t.Cleanup(func() { challengeMemory = previous })
}

func withChallengeSettings(t *testing.T, threshold int, windowSeconds int64) {
	t.Helper()
	previousThreshold := ChallengeTriggerThreshold
	previousWindow := ChallengeWindowSeconds
	ChallengeTriggerThreshold = threshold
	ChallengeWindowSeconds = windowSeconds
	t.Cleanup(func() {
		ChallengeTriggerThreshold = previousThreshold
		ChallengeWindowSeconds = previousWindow
	})
}

// useChallengeMemoryOnly 强制走内存兜底路径。
func useChallengeMemoryOnly(t *testing.T) {
	t.Helper()
	previousEnabled := RedisEnabled
	previousClient := RDB
	RedisEnabled = false
	RDB = nil
	t.Cleanup(func() {
		RedisEnabled = previousEnabled
		RDB = previousClient
	})
}

func useChallengeMiniRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	previousEnabled := RedisEnabled
	previousClient := RDB
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	require.NoError(t, client.Ping(context.Background()).Err())

	RedisEnabled = true
	RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		RedisEnabled = previousEnabled
		RDB = previousClient
	})
	return server
}

func TestChallengeMemoryStoreExpiresWindow(t *testing.T) {
	store := &challengeMemoryStore{entries: make(map[string]challengeEntry)}
	base := time.Unix(1_700_000_000, 0)
	ttl := time.Minute

	assert.Equal(t, int64(1), store.incr("k", ttl, base))
	assert.Equal(t, int64(2), store.incr("k", ttl, base.Add(30*time.Second)))
	assert.Equal(t, int64(2), store.get("k", base.Add(30*time.Second)))

	// 窗口过期后从头计数，而不是在旧值上继续累加
	assert.Equal(t, int64(0), store.get("k", base.Add(2*time.Minute)))
	assert.Equal(t, int64(1), store.incr("k", ttl, base.Add(2*time.Minute)))
}

func TestChallengeMemoryStoreDelete(t *testing.T) {
	store := &challengeMemoryStore{entries: make(map[string]challengeEntry)}
	now := time.Unix(1_700_000_000, 0)

	store.incr("k", time.Minute, now)
	store.del("k")
	assert.Equal(t, int64(0), store.get("k", now))
}

func TestChallengeRequiredOnlyAfterThreshold(t *testing.T) {
	useChallengeMemoryOnly(t)
	resetChallengeMemory(t)
	withChallengeSettings(t, 2, 3600)

	ctx := context.Background()
	const ip = "198.51.100.7"

	// 阈值之下不触发人机验证——这正是国内用户不必加载 Turnstile 的路径
	assert.False(t, ChallengeRequired(ctx, ChallengeScopeAuth, ip))
	assert.Equal(t, int64(1), RecordChallengeStrike(ctx, ChallengeScopeAuth, ip))
	assert.False(t, ChallengeRequired(ctx, ChallengeScopeAuth, ip))
	assert.Equal(t, int64(2), RecordChallengeStrike(ctx, ChallengeScopeAuth, ip))
	assert.True(t, ChallengeRequired(ctx, ChallengeScopeAuth, ip))

	// 登录成功清零后重新回到免验证状态
	ClearChallengeStrikes(ctx, ChallengeScopeAuth, ip)
	assert.Equal(t, int64(0), ChallengeStrikes(ctx, ChallengeScopeAuth, ip))
	assert.False(t, ChallengeRequired(ctx, ChallengeScopeAuth, ip))
}

func TestChallengeScopesAreIndependentPerIP(t *testing.T) {
	useChallengeMemoryOnly(t)
	resetChallengeMemory(t)
	withChallengeSettings(t, 1, 3600)

	ctx := context.Background()
	RecordChallengeStrike(ctx, ChallengeScopeAuth, "198.51.100.8")

	assert.True(t, ChallengeRequired(ctx, ChallengeScopeAuth, "198.51.100.8"))
	assert.False(t, ChallengeRequired(ctx, ChallengeScopeAuth, "198.51.100.9"))
	assert.False(t, ChallengeRequired(ctx, "other", "198.51.100.8"))
}

func TestChallengeThresholdZeroAlwaysRequires(t *testing.T) {
	useChallengeMemoryOnly(t)
	resetChallengeMemory(t)
	withChallengeSettings(t, 0, 3600)

	// 阈值 0 = 关闭渐进,退回旧的无条件校验行为
	assert.True(t, ChallengeRequired(context.Background(), ChallengeScopeAuth, "198.51.100.10"))
}

func TestChallengeEmptyIPIsIgnored(t *testing.T) {
	useChallengeMemoryOnly(t)
	resetChallengeMemory(t)
	withChallengeSettings(t, 1, 3600)

	ctx := context.Background()
	assert.Equal(t, int64(0), RecordChallengeStrike(ctx, ChallengeScopeAuth, ""))
	assert.Equal(t, int64(0), ChallengeStrikes(ctx, ChallengeScopeAuth, ""))
	assert.False(t, ChallengeRequired(ctx, ChallengeScopeAuth, ""))
}

func TestRecordChallengeStrikeSetsRedisTTL(t *testing.T) {
	server := useChallengeMiniRedis(t)
	resetChallengeMemory(t)
	withChallengeSettings(t, 2, 900)

	ctx := context.Background()
	const ip = "203.0.113.5"

	assert.Equal(t, int64(1), RecordChallengeStrike(ctx, ChallengeScopeAuth, ip))
	assert.Equal(t, int64(2), RecordChallengeStrike(ctx, ChallengeScopeAuth, ip))
	assert.Equal(t, int64(2), ChallengeStrikes(ctx, ChallengeScopeAuth, ip))
	assert.True(t, ChallengeRequired(ctx, ChallengeScopeAuth, ip))

	// 计数器必须带 TTL，否则一次失败会让该 IP 被永久要求人机验证
	key := challengeKey(ChallengeScopeAuth, ip)
	assert.Equal(t, 900*time.Second, server.TTL(key))

	// 窗口走完自动归零
	server.FastForward(901 * time.Second)
	assert.Equal(t, int64(0), ChallengeStrikes(ctx, ChallengeScopeAuth, ip))
	assert.False(t, ChallengeRequired(ctx, ChallengeScopeAuth, ip))
}

func TestClearChallengeStrikesRemovesRedisKey(t *testing.T) {
	server := useChallengeMiniRedis(t)
	resetChallengeMemory(t)
	withChallengeSettings(t, 1, 900)

	ctx := context.Background()
	const ip = "203.0.113.6"

	RecordChallengeStrike(ctx, ChallengeScopeAuth, ip)
	ClearChallengeStrikes(ctx, ChallengeScopeAuth, ip)

	assert.False(t, server.Exists(challengeKey(ChallengeScopeAuth, ip)))
	assert.Equal(t, int64(0), ChallengeStrikes(ctx, ChallengeScopeAuth, ip))
}

func TestChallengeFallsBackToMemoryWhenRedisDown(t *testing.T) {
	server := useChallengeMiniRedis(t)
	resetChallengeMemory(t)
	withChallengeSettings(t, 2, 900)

	ctx := context.Background()
	const ip = "203.0.113.7"

	// Redis 挂掉时不能静默丢计数，否则爆破可以靠打挂 Redis 绕过人机验证
	server.Close()
	assert.Equal(t, int64(1), RecordChallengeStrike(ctx, ChallengeScopeAuth, ip))
	assert.Equal(t, int64(2), RecordChallengeStrike(ctx, ChallengeScopeAuth, ip))
	assert.True(t, ChallengeRequired(ctx, ChallengeScopeAuth, ip))
}
