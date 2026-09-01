package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withTurnstileEnabled(t *testing.T, enabled bool) {
	t.Helper()
	previous := common.TurnstileCheckEnabled
	common.TurnstileCheckEnabled = enabled
	t.Cleanup(func() { common.TurnstileCheckEnabled = previous })
}

func withChallengeThreshold(t *testing.T, threshold int) {
	t.Helper()
	previous := common.ChallengeTriggerThreshold
	common.ChallengeTriggerThreshold = threshold
	t.Cleanup(func() { common.ChallengeTriggerThreshold = previous })
}

// newAdaptiveTurnstileRouter 建一个受保护的路由，命中 handler 时返回 204。
func newAdaptiveTurnstileRouter(t *testing.T) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.POST("/protected", AdaptiveTurnstileCheck(common.ChallengeScopeAuth), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	return router
}

func postFrom(router http.Handler, remoteAddr string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/protected", nil)
	request.RemoteAddr = remoteAddr
	router.ServeHTTP(recorder, request)
	return recorder
}

// assertTurnstileRequired 断言响应是“请先过人机验证”，前端据此才去加载 CF 脚本。
func assertTurnstileRequired(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, http.StatusOK, recorder.Code)
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			TurnstileRequired bool `json:"turnstile_required"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.False(t, body.Success)
	assert.True(t, body.Data.TurnstileRequired)
}

func TestAdaptiveTurnstileCheckPassesWhenTurnstileDisabled(t *testing.T) {
	withTurnstileEnabled(t, false)
	withChallengeThreshold(t, 0) // 即便阈值为 0，Turnstile 未开启也不该拦
	router := newAdaptiveTurnstileRouter(t)

	assert.Equal(t, http.StatusNoContent, postFrom(router, "192.0.2.20:1111").Code)
}

func TestAdaptiveTurnstileCheckPassesBelowThreshold(t *testing.T) {
	withTurnstileEnabled(t, true)
	withChallengeThreshold(t, 2)
	router := newAdaptiveTurnstileRouter(t)

	ctx := context.Background()
	const ip = "192.0.2.21"
	common.ClearChallengeStrikes(ctx, common.ChallengeScopeAuth, ip)
	t.Cleanup(func() { common.ClearChallengeStrikes(ctx, common.ChallengeScopeAuth, ip) })

	// 干净 IP：不带 turnstile token 也放行,前端因此不必加载 challenges.cloudflare.com
	assert.Equal(t, http.StatusNoContent, postFrom(router, ip+":1111").Code)

	// 一次可疑行为仍在阈值之下
	common.RecordChallengeStrike(ctx, common.ChallengeScopeAuth, ip)
	assert.Equal(t, http.StatusNoContent, postFrom(router, ip+":1111").Code)
}

func TestAdaptiveTurnstileCheckDemandsTokenAtThreshold(t *testing.T) {
	withTurnstileEnabled(t, true)
	withChallengeThreshold(t, 2)
	router := newAdaptiveTurnstileRouter(t)

	ctx := context.Background()
	const ip = "192.0.2.22"
	common.ClearChallengeStrikes(ctx, common.ChallengeScopeAuth, ip)
	t.Cleanup(func() { common.ClearChallengeStrikes(ctx, common.ChallengeScopeAuth, ip) })

	common.RecordChallengeStrike(ctx, common.ChallengeScopeAuth, ip)
	common.RecordChallengeStrike(ctx, common.ChallengeScopeAuth, ip)

	assertTurnstileRequired(t, postFrom(router, ip+":1111"))

	// 清零后（例如换个真人登录成功）重新免验证
	common.ClearChallengeStrikes(ctx, common.ChallengeScopeAuth, ip)
	assert.Equal(t, http.StatusNoContent, postFrom(router, ip+":1111").Code)
}

func TestAdaptiveTurnstileCheckThresholdZeroAlwaysDemandsToken(t *testing.T) {
	withTurnstileEnabled(t, true)
	withChallengeThreshold(t, 0)
	router := newAdaptiveTurnstileRouter(t)

	// 阈值 0 = 关闭渐进,每个请求都要 token（旧的无条件行为）
	assertTurnstileRequired(t, postFrom(router, "192.0.2.23:1111"))
}
