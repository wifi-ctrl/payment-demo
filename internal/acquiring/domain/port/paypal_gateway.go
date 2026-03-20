package port

import (
	"context"

	"payment-demo/internal/acquiring/domain/model"
)

// PayPalGateway PayPal 支付网关端口（被驱动端口）
// 消费方（payment domain）定义，adapter 层实现，作为 ACL 边界隔离外部 PayPal API 细节
// 与 PaymentGateway 独立：Authorize 入参为 PayPalToken，语义不同，不强行复用
type PayPalGateway interface {
	CaptureRefunder

	// Authorize 验证 PayPal Order 并完成授权
	// token: 前端 JS SDK 返回的 OrderID + PayerID
	// amount: 用于校验 PayPal Order 金额是否与商品价格匹配（防篡改）
	Authorize(ctx context.Context, token model.PayPalToken, amount model.Money) (*PayPalAuthResult, error)
}

// PayPalAuthResult PayPal 授权结果 DTO，接口绑定，放在 port 层
type PayPalAuthResult struct {
	ProviderRef string // PayPal Capture ID，如 "2GG279541U471931P"
	PayerEmail  string // 付款方 PayPal 邮箱，可选，便于记录和对账
}
