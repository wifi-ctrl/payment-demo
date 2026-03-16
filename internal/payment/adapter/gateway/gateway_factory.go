package gateway

import (
	"fmt"
	"log"

	"payment-demo/internal/payment/domain/port"
)

// MockGatewayFactory GatewayFactory 的 Demo 实现。
// 根据 ChannelCredentialView.Secrets 中的商户凭据，构造对应渠道的 Gateway 实例。
// Demo 环境中按凭据 Secrets 初始化 Gateway，并将商户信息注入实例；
// 生产环境替换为真实 Stripe / PayPal SDK 客户端初始化逻辑。
type MockGatewayFactory struct{}

// 编译期接口检查：确保 MockGatewayFactory 实现 port.GatewayFactory。
var _ port.GatewayFactory = (*MockGatewayFactory)(nil)

// NewMockGatewayFactory 构造函数。
func NewMockGatewayFactory() *MockGatewayFactory {
	return &MockGatewayFactory{}
}

// BuildCardGateway 为指定商户构建 Card 支付网关。
// 生产实现：从 cred.Secrets["api_key"] 读取 Stripe API Key，初始化 stripe.Client。
// Demo 实现：将 api_key 注入 MockPaymentGateway，保证不同商户返回不同实例。
func (f *MockGatewayFactory) BuildCardGateway(cred port.ChannelCredentialView) (port.PaymentGateway, error) {
	apiKey, ok := cred.Secrets["api_key"]
	if !ok || apiKey == "" {
		return nil, fmt.Errorf("card gateway: missing api_key for merchant %s", cred.MerchantID)
	}
	log.Printf("[GatewayFactory] BuildCardGateway: merchant=%s, credentialID=%s",
		cred.MerchantID, cred.CredentialID)
	// Demo 环境：将 apiKey 注入 Gateway 实例，使不同商户得到持有不同 apiKey 的独立实例
	return NewMockPaymentGatewayWithKey(apiKey), nil
}

// BuildPayPalGateway 为指定商户构建 PayPal 支付网关。
// 生产实现：从 cred.Secrets["client_id"] + cred.Secrets["client_secret"] 初始化 PayPal OAuth 客户端。
// Demo 实现：将凭据注入 MockPayPalGateway，保证不同商户返回不同实例。
func (f *MockGatewayFactory) BuildPayPalGateway(cred port.ChannelCredentialView) (port.PayPalGateway, error) {
	clientID, okID := cred.Secrets["client_id"]
	clientSecret, okSecret := cred.Secrets["client_secret"]
	if !okID || clientID == "" || !okSecret || clientSecret == "" {
		return nil, fmt.Errorf("paypal gateway: missing client_id or client_secret for merchant %s", cred.MerchantID)
	}
	log.Printf("[GatewayFactory] BuildPayPalGateway: merchant=%s, credentialID=%s",
		cred.MerchantID, cred.CredentialID)
	// Demo 环境：将凭据注入 Gateway 实例，使不同商户得到持有不同凭据的独立实例
	return NewMockPayPalGatewayWithCreds(clientID, clientSecret), nil
}
