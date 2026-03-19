package gateway

import (
	"fmt"
	"log"

	"payment-demo/internal/infra/paypal"
	"payment-demo/internal/infra/stripe"
	"payment-demo/internal/payment/domain/port"
)

// MultiChannelGatewayFactory 多渠道网关工厂。
// 持有各渠道共享的 infra 客户端，按渠道类型构建对应的 Gateway 实例。
// Card → StripeGatewayAdapter（via stripe.Client）
// PayPal → PayPalGatewayAdapter（via paypal.Client）
type MultiChannelGatewayFactory struct {
	stripeClient *stripe.Client
	paypalClient *paypal.Client
}

var _ port.GatewayFactory = (*MultiChannelGatewayFactory)(nil)

// NewMultiChannelGatewayFactory 构造函数，注入各渠道的共享客户端。
func NewMultiChannelGatewayFactory(stripeClient *stripe.Client, paypalClient *paypal.Client) *MultiChannelGatewayFactory {
	return &MultiChannelGatewayFactory{
		stripeClient: stripeClient,
		paypalClient: paypalClient,
	}
}

// BuildCardGateway 为指定商户构建 Stripe Card 支付网关。
func (f *MultiChannelGatewayFactory) BuildCardGateway(cred port.ChannelCredentialView) (port.PaymentGateway, error) {
	apiKey, ok := cred.Secrets["api_key"]
	if !ok || apiKey == "" {
		return nil, fmt.Errorf("card gateway: missing api_key for merchant %s", cred.MerchantID)
	}
	log.Printf("[GatewayFactory] BuildCardGateway: merchant=%s, credentialID=%s",
		cred.MerchantID, cred.CredentialID)
	return NewStripeGatewayAdapter(f.stripeClient, apiKey), nil
}

// BuildPayPalGateway 为指定商户构建 PayPal 支付网关。
func (f *MultiChannelGatewayFactory) BuildPayPalGateway(cred port.ChannelCredentialView) (port.PayPalGateway, error) {
	clientID, okID := cred.Secrets["client_id"]
	clientSecret, okSecret := cred.Secrets["client_secret"]
	if !okID || clientID == "" || !okSecret || clientSecret == "" {
		return nil, fmt.Errorf("paypal gateway: missing client_id or client_secret for merchant %s", cred.MerchantID)
	}
	log.Printf("[GatewayFactory] BuildPayPalGateway: merchant=%s, credentialID=%s",
		cred.MerchantID, cred.CredentialID)
	return NewPayPalGatewayAdapter(f.paypalClient, clientID, clientSecret), nil
}
