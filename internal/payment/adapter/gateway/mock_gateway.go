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

// MockPaymentGateway 模拟外部支付网关（如 Stripe）
// 这就是 ACL — 在这里完成领域模型与外部系统模型的双向翻译
type MockPaymentGateway struct{}

// 编译期检查接口实现
var _ port.PaymentGateway = (*MockPaymentGateway)(nil)

func NewMockPaymentGateway() *MockPaymentGateway {
	return &MockPaymentGateway{}
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
