package gateway

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"

	"payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// MockPaymentGateway 模拟外部 Card 支付网关（如 Stripe）。
// Demo 环境中作为真实适配器使用；生产中替换为真实 Stripe/Adyen 客户端。
// apiKey 字段持有商户专属密钥，确保不同商户的 Gateway 实例互相独立（指针不同）。
type MockPaymentGateway struct {
	apiKey string // 商户专属 API Key（Demo 中只用于区分实例，不真实调用外部 API）
}

// 编译期检查接口实现
var _ port.PaymentGateway = (*MockPaymentGateway)(nil)

// NewMockPaymentGateway 使用空 API Key 构造（向后兼容，直接测试用）。
func NewMockPaymentGateway() *MockPaymentGateway {
	return &MockPaymentGateway{apiKey: fmt.Sprintf("demo_%d", rand.Int63())}
}

// NewMockPaymentGatewayWithKey 使用指定 API Key 构造，由 GatewayFactory 调用。
// 每次调用均返回新指针，保证不同商户实例互相独立。
func NewMockPaymentGatewayWithKey(apiKey string) *MockPaymentGateway {
	return &MockPaymentGateway{apiKey: apiKey}
}

func (g *MockPaymentGateway) Authorize(_ context.Context, token model.CardToken, amount model.Money) (*port.GatewayAuthResult, error) {
	log.Printf("[MockGateway] Authorize: token=%s, amount=%s", token.TokenID, amount.String())

	if strings.HasPrefix(token.TokenID, "tok_decline") {
		return nil, fmt.Errorf("card declined: insufficient funds")
	}

	return &port.GatewayAuthResult{
		ProviderRef: fmt.Sprintf("pi_%d", rand.Int63()),
		AuthCode:    fmt.Sprintf("AUTH_%06d", rand.Intn(999999)),
	}, nil
}

func (g *MockPaymentGateway) Capture(_ context.Context, providerRef string, amount model.Money) error {
	log.Printf("[MockGateway] Capture: ref=%s, amount=%s", providerRef, amount.String())
	return nil
}

func (g *MockPaymentGateway) Refund(_ context.Context, providerRef string, amount model.Money) error {
	log.Printf("[MockGateway] Refund: ref=%s, amount=%s", providerRef, amount.String())
	return nil
}
