package setting

var (
	// XorPayEnabled 总开关；关闭后不再向前端暴露 XorPay 支付方式，webhook 也会拒收。
	XorPayEnabled bool
	// XorPayAid XorPay 后台的应用 ID，出现在下单/查询的 URL 路径上。
	XorPayAid string
	// XorPayAppSecret 参与所有签名计算，绝不下发给前端。
	XorPayAppSecret string
	// XorPayNotifyUrl 留空时使用 service.GetCallbackAddress() + /api/xorpay/notify。
	XorPayNotifyUrl string
	// XorPayMinTopUp XorPay 渠道单独的最低充值数量。
	XorPayMinTopUp int = 1
	// XorPayExpire 下单后二维码的有效期（秒），XorPay 默认 7200。
	XorPayExpire int = 900
)
