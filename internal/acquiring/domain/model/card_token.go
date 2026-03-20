package model

// CardToken 卡令牌值对象：PSP 原生 token 来自前端；自建 ct_* 的 last4/brand 应由 card 服务在 Authorize 前解析写入
type CardToken struct {
	TokenID string // 如 "tok_visa_4242"
	Last4   string // 卡号后四位
	Brand   string // Visa / Mastercard / ...
}
