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

// MockPayPalGateway 模拟 PayPal 外部网关（ACL 边界）。
// Demo 环境中作为真实适配器使用；生产中替换为真实 PayPal REST API 客户端。
//
// clientID / clientSecret 字段持有商户专属凭据，确保不同商户的 Gateway 实例互相独立（指针不同）。
//
// 模拟规则：
//   - OrderID 以 "EC-DECLINE" 开头 → Authorize 返回 ErrPayPalTokenInvalid（模拟无效/过期 token）
//   - 其他 OrderID → 授权成功，返回随机 ProviderRef
type MockPayPalGateway struct {
	clientID     string // 商户 PayPal Client ID
	clientSecret string // 商户 PayPal Client Secret
}

// 编译期接口检查：确保 MockPayPalGateway 实现了 port.PayPalGateway
var _ port.PayPalGateway = (*MockPayPalGateway)(nil)

// NewMockPayPalGateway 使用随机凭据构造（向后兼容，直接测试用）。
func NewMockPayPalGateway() *MockPayPalGateway {
	return &MockPayPalGateway{
		clientID:     fmt.Sprintf("demo_client_%d", rand.Int63()),
		clientSecret: fmt.Sprintf("demo_secret_%d", rand.Int63()),
	}
}

// NewMockPayPalGatewayWithCreds 使用指定凭据构造，由 GatewayFactory 调用。
// 每次调用均返回新指针，保证不同商户实例互相独立。
func NewMockPayPalGatewayWithCreds(clientID, clientSecret string) *MockPayPalGateway {
	return &MockPayPalGateway{clientID: clientID, clientSecret: clientSecret}
}

// Authorize 模拟 PayPal Order 授权
func (g *MockPayPalGateway) Authorize(_ context.Context, token model.PayPalToken, amount model.Money) (*port.PayPalAuthResult, error) {
	log.Printf("[MockPayPalGateway] Authorize: orderID=%s, payerID=%s, amount=%s",
		token.OrderID, token.PayerID, amount.String())

	// EC-DECLINE 前缀模拟无效 / 过期的 PayPal Order
	if strings.HasPrefix(token.OrderID, "EC-DECLINE") {
		return nil, model.ErrPayPalTokenInvalid
	}

	return &port.PayPalAuthResult{
		ProviderRef: fmt.Sprintf("CAPTURE-%d", rand.Int63()),
		PayerEmail:  "buyer@example.com",
	}, nil
}

// Capture 模拟从已授权 PayPal Order 扣款
func (g *MockPayPalGateway) Capture(_ context.Context, providerRef string, amount model.Money) error {
	log.Printf("[MockPayPalGateway] Capture: ref=%s, amount=%s", providerRef, amount.String())
	return nil
}

// Refund 模拟对已扣款 PayPal Order 退款
func (g *MockPayPalGateway) Refund(_ context.Context, providerRef string, amount model.Money) error {
	log.Printf("[MockPayPalGateway] Refund: ref=%s, amount=%s", providerRef, amount.String())
	return nil
}
