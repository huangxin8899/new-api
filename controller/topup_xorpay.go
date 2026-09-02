package controller

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

const (
	xorPayBaseURL     = "https://xorpay.com"
	xorPayHTTPTimeout = 15 * time.Second

	// XorPay 的 pay_type 取值，仅开放 PC 网页扫码的两种。
	xorPayTypeNative = "native"
	xorPayTypeAlipay = "alipay"

	// 订单查询返回的状态，仅 success 视为已结算可入账。
	xorPayOrderStatusSuccess = "success"
)

// xorPayMethods 是暴露给前端的支付方式列表；method 与 model.PaymentMethod* 一一对应。
var xorPayMethods = []map[string]string{
	{
		"name":     "微信扫码",
		"type":     model.PaymentMethodXorPayNative,
		"pay_type": xorPayTypeNative,
		"color":    "#07C160",
	},
	{
		"name":     "支付宝扫码",
		"type":     model.PaymentMethodXorPayAlipay,
		"pay_type": xorPayTypeAlipay,
		"color":    "#1677FF",
	},
}

// xorPayResolveType 把前端传来的 method 归一化为 XorPay 的 pay_type。
// 只认服务端白名单，避免客户端塞入 jsapi / barcode 等未对接的场景。
func xorPayResolveType(method string) (payType string, ok bool) {
	switch strings.TrimSpace(method) {
	case model.PaymentMethodXorPayNative, xorPayTypeNative:
		return xorPayTypeNative, true
	case model.PaymentMethodXorPayAlipay, xorPayTypeAlipay:
		return xorPayTypeAlipay, true
	default:
		return "", false
	}
}

func xorPayMethodOf(payType string) string {
	if payType == xorPayTypeAlipay {
		return model.PaymentMethodXorPayAlipay
	}
	return model.PaymentMethodXorPayNative
}

// xorPaySign 按 XorPay 约定拼接原始值后取 32 位小写 MD5。
// 参数顺序由调用方保证，不做排序、不做 URL encode。
func xorPaySign(parts ...string) string {
	sum := md5.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(sum[:])
}

// xorPayPrice 统一成两位小数字符串，XorPay 对 "50" 这类写法会验签失败。
func xorPayPrice(money float64) string {
	return decimal.NewFromFloat(money).RoundBank(2).StringFixed(2)
}

func xorPayNotifyUrl() string {
	if u := strings.TrimSpace(setting.XorPayNotifyUrl); u != "" {
		return u
	}
	return service.GetCallbackAddress() + "/api/xorpay/notify"
}

func xorPayExpireSeconds() int {
	if setting.XorPayExpire > 0 {
		return setting.XorPayExpire
	}
	return 900
}

func xorPayMinTopUp() int64 {
	if setting.XorPayMinTopUp > 0 {
		return int64(setting.XorPayMinTopUp)
	}
	return getMinTopup()
}

// xorPayPostForm 发送 application/x-www-form-urlencoded 请求并解析 JSON 响应。
func xorPayPostForm(ctx context.Context, endpoint string, form url.Values) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return xorPayDo(req)
}

func xorPayGet(ctx context.Context, endpoint string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	return xorPayDo(req)
}

func xorPayDo(req *http.Request) (map[string]any, error) {
	client := service.GetHttpClient()
	ctx, cancel := context.WithTimeout(req.Context(), xorPayHTTPTimeout)
	defer cancel()
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var parsed map[string]any
	if err := common.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("xorpay 响应解析失败 status=%d body=%q", resp.StatusCode, string(body))
	}
	return parsed, nil
}

func xorPayString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

type XorPayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

// RequestXorPayAmount 预览实付金额，口径与易支付一致（人民币）。
func RequestXorPayAmount(c *gin.Context) {
	var req XorPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	minTopUp := xorPayMinTopUp()
	if req.Amount < minTopUp {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", minTopUp)})
		return
	}
	id := c.GetInt("id")
	if rejectInvalidTopUpQuota(c, id, req.Amount) {
		return
	}
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	payMoney := getPayMoney(req.Amount, group)
	if payMoney < 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": xorPayPrice(payMoney)})
}

// RequestXorPayPay 创建 XorPay 订单并返回二维码内容，由前端渲染成二维码。
func RequestXorPayPay(c *gin.Context) {
	if !isXorPayTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "XorPay 支付未启用"})
		return
	}

	var req XorPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	payType, ok := xorPayResolveType(req.PaymentMethod)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付方式不存在"})
		return
	}
	minTopUp := xorPayMinTopUp()
	if req.Amount < minTopUp {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", minTopUp)})
		return
	}
	id := c.GetInt("id")
	if rejectInvalidTopUpQuota(c, id, req.Amount) {
		return
	}
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	payMoney := getPayMoney(req.Amount, group)
	if payMoney < 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	// order_id 需全局唯一；带上用户与时间戳便于对账。
	tradeNo := fmt.Sprintf("XOR%dNO%s%d", id, common.GetRandomString(6), time.Now().Unix())
	price := xorPayPrice(payMoney)
	notifyUrl := xorPayNotifyUrl()
	name := fmt.Sprintf("TUC%d", req.Amount)

	// Token 展示模式下前端传的是 tokens，落库统一归一化成金额单位，
	// 与易支付保持一致，避免 RechargeXorPay 再乘一次 QuotaPerUnit。
	amount := req.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amount = decimal.NewFromInt(amount).Div(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
	}

	topUp := &model.TopUp{
		UserId:          id,
		Amount:          amount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   xorPayMethodOf(payType),
		PaymentProvider: model.PaymentProviderXorPay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("XorPay 创建充值订单失败 user_id=%d trade_no=%s amount=%d error=%q", id, tradeNo, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	form := url.Values{}
	form.Set("name", name)
	form.Set("pay_type", payType)
	form.Set("price", price)
	form.Set("order_id", tradeNo)
	form.Set("notify_url", notifyUrl)
	form.Set("order_uid", strconv.Itoa(id))
	form.Set("expire", strconv.Itoa(xorPayExpireSeconds()))
	// 签名字段顺序固定：name + pay_type + price + order_id + notify_url + app_secret
	form.Set("sign", xorPaySign(name, payType, price, tradeNo, notifyUrl, setting.XorPayAppSecret))

	endpoint := fmt.Sprintf("%s/api/pay/%s", xorPayBaseURL, url.PathEscape(strings.TrimSpace(setting.XorPayAid)))
	resp, err := xorPayPostForm(c.Request.Context(), endpoint, form)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("XorPay 下单请求失败 user_id=%d trade_no=%s error=%q", id, tradeNo, err.Error()))
		topUp.Status = common.TopUpStatusFailed
		_ = topUp.Update()
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	status := xorPayString(resp, "status")
	if status != "ok" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("XorPay 下单业务失败 user_id=%d trade_no=%s status=%s response=%q", id, tradeNo, status, common.GetJsonString(resp)))
		topUp.Status = common.TopUpStatusFailed
		_ = topUp.Update()
		// status 是 XorPay 侧的错误码（sign_error / fee_error 等），仅记录日志不外泄。
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	info, _ := resp["info"].(map[string]any)
	qr := xorPayString(info, "qr")
	if qr == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("XorPay 下单未返回二维码 user_id=%d trade_no=%s response=%q", id, tradeNo, common.GetJsonString(resp)))
		topUp.Status = common.TopUpStatusFailed
		_ = topUp.Update()
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("XorPay 充值订单创建成功 user_id=%d trade_no=%s pay_type=%s amount=%d money=%s", id, tradeNo, payType, req.Amount, price))
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"qr_content": qr,
			"trade_no":   tradeNo,
			"pay_type":   payType,
			"money":      price,
			"expire":     xorPayExpireSeconds(),
		},
	})
}

// xorPayConfirmPaid 用商户订单号回查 XorPay，确认订单确实已结算。
// 回调验签之外的二次确认，防止伪造回调或 fee_error 状态被误当成功。
func xorPayConfirmPaid(ctx context.Context, tradeNo string) (bool, string, error) {
	sign := xorPaySign(tradeNo, setting.XorPayAppSecret)
	endpoint := fmt.Sprintf("%s/api/query2/%s?order_id=%s&sign=%s",
		xorPayBaseURL,
		url.PathEscape(strings.TrimSpace(setting.XorPayAid)),
		url.QueryEscape(tradeNo),
		url.QueryEscape(sign),
	)
	resp, err := xorPayGet(ctx, endpoint)
	if err != nil {
		return false, "", err
	}
	status := xorPayString(resp, "status")
	return status == xorPayOrderStatusSuccess, status, nil
}

// XorPayNotify 处理 XorPay 支付回调（application/x-www-form-urlencoded）。
// 幂等性由 model.RechargeXorPay 的行锁事务保证；响应正文必须含 success，否则 XorPay 会重推 6 次。
func XorPayNotify(c *gin.Context) {
	if !isXorPayWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("XorPay webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	if err := c.Request.ParseForm(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("XorPay webhook 表单解析失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	aoid := c.Request.PostForm.Get("aoid")
	tradeNo := c.Request.PostForm.Get("order_id")
	payPrice := c.Request.PostForm.Get("pay_price")
	payTime := c.Request.PostForm.Get("pay_time")
	sign := c.Request.PostForm.Get("sign")
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("XorPay webhook 收到请求 aoid=%s trade_no=%s pay_price=%s pay_time=%s client_ip=%s", aoid, tradeNo, payPrice, payTime, c.ClientIP()))

	if tradeNo == "" || sign == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("XorPay webhook 参数缺失 trade_no=%s client_ip=%s", tradeNo, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	// 验签字段顺序固定：aoid + order_id + pay_price + pay_time + app_secret
	expected := xorPaySign(aoid, tradeNo, payPrice, payTime, setting.XorPayAppSecret)
	if !strings.EqualFold(expected, strings.TrimSpace(sign)) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("XorPay webhook 验签失败 trade_no=%s aoid=%s client_ip=%s", tradeNo, aoid, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	if !xorPaySettle(c, tradeNo, payPrice) {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	_, _ = c.Writer.Write([]byte("success"))
}

// xorPaySettle 二次回查 + 金额校验 + 入账。调用方需先持有订单锁。
func xorPaySettle(c *gin.Context, tradeNo string, payPrice string) bool {
	topUp := model.GetTopUpByTradeNo(tradeNo)
	if topUp == nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("XorPay 回调订单不存在 trade_no=%s client_ip=%s", tradeNo, c.ClientIP()))
		return false
	}
	if topUp.PaymentProvider != model.PaymentProviderXorPay {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("XorPay 订单支付网关不匹配 trade_no=%s provider=%s client_ip=%s", tradeNo, topUp.PaymentProvider, c.ClientIP()))
		return false
	}
	if topUp.Status == common.TopUpStatusSuccess {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("XorPay 重复回调幂等忽略 trade_no=%s client_ip=%s", tradeNo, c.ClientIP()))
		return true
	}

	// 实付金额不得少于下单金额，防止改价。
	if payPrice != "" {
		paid, err := decimal.NewFromString(strings.TrimSpace(payPrice))
		if err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("XorPay 回调金额无法解析 trade_no=%s pay_price=%q", tradeNo, payPrice))
			return false
		}
		if paid.LessThan(decimal.NewFromFloat(topUp.Money).RoundBank(2)) {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("XorPay 回调金额小于订单金额 trade_no=%s pay_price=%s order_money=%.2f", tradeNo, payPrice, topUp.Money))
			return false
		}
	}

	paid, status, err := xorPayConfirmPaid(c.Request.Context(), tradeNo)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("XorPay 订单回查失败 trade_no=%s error=%q", tradeNo, err.Error()))
		return false
	}
	if !paid {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("XorPay 订单回查状态非成功，跳过入账 trade_no=%s status=%s", tradeNo, status))
		return false
	}

	alreadyDone, err := model.RechargeXorPay(tradeNo, "", c.ClientIP())
	if err != nil {
		switch {
		case errors.Is(err, model.ErrTopUpNotFound):
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("XorPay 入账时订单不存在 trade_no=%s", tradeNo))
		case errors.Is(err, model.ErrPaymentMethodMismatch):
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("XorPay 入账时网关不匹配 trade_no=%s", tradeNo))
		case errors.Is(err, model.ErrTopUpStatusInvalid):
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("XorPay 入账时订单状态非法 trade_no=%s", tradeNo))
		default:
			logger.LogError(c.Request.Context(), fmt.Sprintf("XorPay 充值处理失败 trade_no=%s error=%q", tradeNo, err.Error()))
		}
		return false
	}
	if alreadyDone {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("XorPay 重复回调幂等忽略 trade_no=%s", tradeNo))
	} else {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("XorPay 充值成功 trade_no=%s client_ip=%s", tradeNo, c.ClientIP()))
	}
	return true
}

// QueryXorPayOrder 供前端扫码期间轮询：返回本地订单状态，
// 若仍处于 pending 则主动回查 XorPay 一次，作为回调延迟/丢失时的兜底。
func QueryXorPayOrder(c *gin.Context) {
	tradeNo := strings.TrimSpace(c.Query("trade_no"))
	if tradeNo == "" {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	userId := c.GetInt("id")
	topUp := model.GetTopUpByTradeNo(tradeNo)
	// 越权查询与订单不存在返回同样的错误，避免探测他人订单号。
	if topUp == nil || topUp.UserId != userId || topUp.PaymentProvider != model.PaymentProviderXorPay {
		common.ApiErrorMsg(c, "订单不存在")
		return
	}

	if topUp.Status == common.TopUpStatusPending && isXorPayTopUpEnabled() {
		LockOrder(tradeNo)
		if xorPaySettle(c, tradeNo, "") {
			if refreshed := model.GetTopUpByTradeNo(tradeNo); refreshed != nil {
				topUp = refreshed
			}
		}
		UnlockOrder(tradeNo)
	}

	common.ApiSuccess(c, gin.H{
		"trade_no": topUp.TradeNo,
		"status":   topUp.Status,
	})
}
