package middleware

import (
	"net/http"
	"net/url"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type turnstileCheckResponse struct {
	Success bool `json:"success"`
}

// writeTurnstileRequired 告诉前端“这次必须过人机验证”。前端据此才去加载
// challenges.cloudflare.com 并渲染 widget，然后带着 token 重试——正常用户
// 走不到这条分支，也就不会为那个域名的延迟买单。
func writeTurnstileRequired(c *gin.Context, message string) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": message,
		"data": gin.H{
			"turnstile_required": true,
		},
	})
	c.Abort()
}

// verifyTurnstile 执行 Cloudflare siteverify。返回 false 时响应已写好。
func verifyTurnstile(c *gin.Context) bool {
	response := c.Query("turnstile")
	if response == "" {
		writeTurnstileRequired(c, "请完成人机验证")
		return false
	}
	rawRes, err := http.PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", url.Values{
		"secret":   {common.TurnstileSecretKey},
		"response": {response},
		"remoteip": {c.ClientIP()},
	})
	if err != nil {
		common.SysLog(err.Error())
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		c.Abort()
		return false
	}
	defer rawRes.Body.Close()
	var res turnstileCheckResponse
	err = common.DecodeJson(rawRes.Body, &res)
	if err != nil {
		common.SysLog(err.Error())
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		c.Abort()
		return false
	}
	if !res.Success {
		writeTurnstileRequired(c, "Turnstile 校验失败，请刷新重试！")
		return false
	}
	return true
}

// TurnstileCheck 无条件校验 Turnstile，用于本来就在登录态下、
// 不需要考虑首屏加载成本的接口（如签到）。
func TurnstileCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if common.TurnstileCheckEnabled && !verifyTurnstile(c) {
			return
		}
		c.Next()
	}
}

// AdaptiveTurnstileCheck 只在该 IP 的可疑度越过阈值后才要求人机验证。
// 可疑度由各 handler 通过 common.RecordChallengeStrike 累加（登录失败、
// 注册、发验证邮件），登录成功则清零。
func AdaptiveTurnstileCheck(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.TurnstileCheckEnabled {
			c.Next()
			return
		}
		if !common.ChallengeRequired(c.Request.Context(), scope, c.ClientIP()) {
			c.Next()
			return
		}
		if !verifyTurnstile(c) {
			return
		}
		c.Next()
	}
}
