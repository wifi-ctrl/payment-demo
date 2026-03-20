package stripe

import (
	"context"

	"payment-demo/internal/acquiring/domain/model"
	"payment-demo/internal/acquiring/domain/port"
)

// GatewayAdapter 基于 Stripe PaymentIntents API 的 PaymentGateway 实现。
// 通过同包的 Client 与 Stripe 通信，将 Stripe 响应翻译为 payment 领域模型（ACL）。
//
// apiKey 持有商户专属密钥，不同商户的 GatewayAdapter 实例持有不同 apiKey，
// 保证多商户隔离。
type GatewayAdapter struct {
	client *Client
	apiKey string
}

var _ port.PaymentGateway = (*GatewayAdapter)(nil)

func NewGatewayAdapter(client *Client, apiKey string) *GatewayAdapter {
	return &GatewayAdapter{client: client, apiKey: apiKey}
}

func (g *GatewayAdapter) Authorize(_ context.Context, token model.CardToken, amount model.Money) (*port.GatewayAuthResult, error) {
	result, err := g.client.CreatePaymentIntent(PaymentIntentParams{
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

func (g *GatewayAdapter) Capture(_ context.Context, providerRef string, amount model.Money) error {
	return g.client.CapturePaymentIntent(providerRef, CaptureParams{
		Amount:   amount.Amount,
		Currency: amount.Currency,
		APIKey:   g.apiKey,
	})
}

func (g *GatewayAdapter) Refund(_ context.Context, providerRef string, amount model.Money) error {
	return g.client.CreateRefund(RefundParams{
		PaymentIntentID: providerRef,
		Amount:          amount.Amount,
		Currency:        amount.Currency,
		APIKey:          g.apiKey,
	})
}
