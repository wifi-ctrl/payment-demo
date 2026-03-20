package paypal

import (
	"context"

	"payment-demo/internal/acquiring/domain/model"
	"payment-demo/internal/acquiring/domain/port"
)

// GatewayAdapter 基于 PayPal Orders/Captures API 的 PayPalGateway 实现。
// 通过同包的 Client 与 PayPal 通信，将 PayPal 响应翻译为 payment 领域模型（ACL）。
//
// clientID / clientSecret 持有商户专属 PayPal 凭据，保证多商户隔离。
type GatewayAdapter struct {
	client       *Client
	clientID     string
	clientSecret string
}

var _ port.PayPalGateway = (*GatewayAdapter)(nil)

func NewGatewayAdapter(client *Client, clientID, clientSecret string) *GatewayAdapter {
	return &GatewayAdapter{
		client:       client,
		clientID:     clientID,
		clientSecret: clientSecret,
	}
}

func (g *GatewayAdapter) Authorize(_ context.Context, token model.PayPalToken, _ model.Money) (*port.PayPalAuthResult, error) {
	result, err := g.client.AuthorizeOrder(AuthorizeOrderParams{
		OrderID:      token.OrderID,
		PayerID:      token.PayerID,
		ClientID:     g.clientID,
		ClientSecret: g.clientSecret,
	})
	if err != nil {
		return nil, err
	}

	return &port.PayPalAuthResult{
		ProviderRef: result.CaptureID,
		PayerEmail:  result.PayerEmail,
	}, nil
}

func (g *GatewayAdapter) Capture(_ context.Context, providerRef string, amount model.Money) error {
	return g.client.CapturePayment(providerRef, CaptureParams{
		Amount:       amount.Amount,
		Currency:     amount.Currency,
		ClientID:     g.clientID,
		ClientSecret: g.clientSecret,
	})
}

func (g *GatewayAdapter) Refund(_ context.Context, providerRef string, amount model.Money) error {
	return g.client.RefundPayment(providerRef, RefundParams{
		Amount:       amount.Amount,
		Currency:     amount.Currency,
		ClientID:     g.clientID,
		ClientSecret: g.clientSecret,
	})
}
