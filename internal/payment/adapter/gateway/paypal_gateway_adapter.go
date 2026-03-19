package gateway

import (
	"context"

	"payment-demo/internal/infra/paypal"
	"payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// PayPalGatewayAdapter 基于 PayPal Orders/Captures API 的 PayPalGateway 实现。
// 通过共享的 paypal.Client 与 PayPal 通信，将 PayPal 响应翻译为 payment 领域模型（ACL）。
//
// clientID / clientSecret 持有商户专属 PayPal 凭据，保证多商户隔离。
type PayPalGatewayAdapter struct {
	client       *paypal.Client
	clientID     string
	clientSecret string
}

var _ port.PayPalGateway = (*PayPalGatewayAdapter)(nil)

// NewPayPalGatewayAdapter 构造函数，注入共享 PayPal 客户端和商户专属凭据。
func NewPayPalGatewayAdapter(client *paypal.Client, clientID, clientSecret string) *PayPalGatewayAdapter {
	return &PayPalGatewayAdapter{
		client:       client,
		clientID:     clientID,
		clientSecret: clientSecret,
	}
}

// Authorize 调用 PayPal Orders API 授权订单。
// ACL 翻译：paypal.AuthorizeOrderResult → port.PayPalAuthResult。
func (g *PayPalGatewayAdapter) Authorize(_ context.Context, token model.PayPalToken, _ model.Money) (*port.PayPalAuthResult, error) {
	result, err := g.client.AuthorizeOrder(paypal.AuthorizeOrderParams{
		OrderID:      token.OrderID,
		PayerID:      token.PayerID,
		ClientID:     g.clientID,
		ClientSecret: g.clientSecret,
	})
	if err != nil {
		return nil, err
	}

	// ACL 翻译：paypal.AuthorizeOrderResult → payment 领域 DTO
	return &port.PayPalAuthResult{
		ProviderRef: result.CaptureID,
		PayerEmail:  result.PayerEmail,
	}, nil
}

// Capture 扣款。
func (g *PayPalGatewayAdapter) Capture(_ context.Context, providerRef string, amount model.Money) error {
	return g.client.CapturePayment(providerRef, paypal.CaptureParams{
		Amount:       amount.Amount,
		Currency:     amount.Currency,
		ClientID:     g.clientID,
		ClientSecret: g.clientSecret,
	})
}

// Refund 退款。
func (g *PayPalGatewayAdapter) Refund(_ context.Context, providerRef string, amount model.Money) error {
	return g.client.RefundPayment(providerRef, paypal.RefundParams{
		Amount:       amount.Amount,
		Currency:     amount.Currency,
		ClientID:     g.clientID,
		ClientSecret: g.clientSecret,
	})
}
