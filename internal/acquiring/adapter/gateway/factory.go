package gateway

import (
	"fmt"
	"log"

	"payment-demo/internal/acquiring/adapter/gateway/paypal"
	"payment-demo/internal/acquiring/adapter/gateway/stripe"
	"payment-demo/internal/acquiring/domain/port"
)

// MultiChannelGatewayFactory 多渠道网关工厂。
// 持有各渠道的 HTTP Client，按渠道类型构建对应的 Gateway 实例。
type MultiChannelGatewayFactory struct {
	stripeClient *stripe.Client
	paypalClient *paypal.Client
}

var _ port.GatewayFactory = (*MultiChannelGatewayFactory)(nil)

func NewMultiChannelGatewayFactory(stripeClient *stripe.Client, paypalClient *paypal.Client) *MultiChannelGatewayFactory {
	return &MultiChannelGatewayFactory{
		stripeClient: stripeClient,
		paypalClient: paypalClient,
	}
}

func (f *MultiChannelGatewayFactory) BuildCardGateway(cred port.ChannelCredentialView) (port.PaymentGateway, error) {
	apiKey, ok := cred.Secrets["api_key"]
	if !ok || apiKey == "" {
		return nil, fmt.Errorf("card gateway: missing api_key for merchant %s", cred.MerchantID)
	}
	log.Printf("[GatewayFactory] BuildCardGateway: merchant=%s, credentialID=%s",
		cred.MerchantID, cred.CredentialID)
	return stripe.NewGatewayAdapter(f.stripeClient, apiKey), nil
}

func (f *MultiChannelGatewayFactory) BuildPayPalGateway(cred port.ChannelCredentialView) (port.PayPalGateway, error) {
	clientID, okID := cred.Secrets["client_id"]
	clientSecret, okSecret := cred.Secrets["client_secret"]
	if !okID || clientID == "" || !okSecret || clientSecret == "" {
		return nil, fmt.Errorf("paypal gateway: missing client_id or client_secret for merchant %s", cred.MerchantID)
	}
	log.Printf("[GatewayFactory] BuildPayPalGateway: merchant=%s, credentialID=%s",
		cred.MerchantID, cred.CredentialID)
	return paypal.NewGatewayAdapter(f.paypalClient, clientID, clientSecret), nil
}
