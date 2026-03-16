package model

// PaymentMethod 支付方式枚举，区分同一笔交易走哪条 Gateway 路径
type PaymentMethod string

const (
	PaymentMethodCard   PaymentMethod = "CARD"
	PaymentMethodPayPal PaymentMethod = "PAYPAL"
)
