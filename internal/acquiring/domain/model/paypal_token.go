package model

// PayPalToken PayPal 前端 JS SDK 返回的一次性授权令牌
// 类比 CardToken，由前端 tokenization 生成，交由 PayPalGateway 验证
type PayPalToken struct {
	OrderID string // PayPal Order ID，如 "5O190127TN364715T"
	PayerID string // PayPal Payer ID，如 "FSMVU44LF3YUS"
}
