package gateway

import (
	"context"

	"payment-demo/internal/infra/stripe"
	"payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// StripeGatewayAdapter 基于 Stripe PaymentIntents API 的 PaymentGateway 实现。
// 通过共享的 stripe.Client 与 Stripe 通信，将 Stripe 响应翻译为 payment 领域模型（ACL）。
//
// adapter 只做 ACL 翻译（stripe.PaymentIntentResult → port.GatewayAuthResult），
// 不关心 HTTP URL、请求格式等 Stripe API 细节 — 那些封装在 stripe.Client 中。
//
// apiKey 持有商户专属密钥 — 不同商户的 StripeGatewayAdapter 实例持有不同 apiKey，
// 保证多商户隔离。
type StripeGatewayAdapter struct {
	client *stripe.Client
	apiKey string
}

var _ port.PaymentGateway = (*StripeGatewayAdapter)(nil)

// NewStripeGatewayAdapter 构造函数，注入共享 Stripe 客户端和商户专属 API Key。
func NewStripeGatewayAdapter(client *stripe.Client, apiKey string) *StripeGatewayAdapter {
	return &StripeGatewayAdapter{client: client, apiKey: apiKey}
}

// Authorize 创建 Stripe PaymentIntent 授权。
// ACL 翻译：stripe.PaymentIntentResult → port.GatewayAuthResult。
func (g *StripeGatewayAdapter) Authorize(_ context.Context, token model.CardToken, amount model.Money) (*port.GatewayAuthResult, error) {
	result, err := g.client.CreatePaymentIntent(stripe.PaymentIntentParams{
		Token:    token.TokenID,
		Amount:   amount.Amount,
		Currency: amount.Currency,
		APIKey:   g.apiKey,
	})
	if err != nil {
		return nil, err
	}

	return &port.GatewayAuthResult{
		ProviderRef:    result.ID,
		AuthCode:       result.AuthCode,
		RecurringToken: result.RecurringToken,
		Channel:        "stripe",
	}, nil
}

// Capture 扣款。
func (g *StripeGatewayAdapter) Capture(_ context.Context, providerRef string, amount model.Money) error {
	return g.client.CapturePaymentIntent(providerRef, stripe.CaptureParams{
		Amount:   amount.Amount,
		Currency: amount.Currency,
		APIKey:   g.apiKey,
	})
}

// Refund 退款。
func (g *StripeGatewayAdapter) Refund(_ context.Context, providerRef string, amount model.Money) error {
	return g.client.CreateRefund(stripe.RefundParams{
		PaymentIntentID: providerRef,
		Amount:          amount.Amount,
		Currency:        amount.Currency,
		APIKey:          g.apiKey,
	})
}
