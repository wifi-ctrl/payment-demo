package port

import (
	"context"

	"payment-demo/internal/payment/domain/model"
)

// PaymentGateway 支付网关端口（被驱动端口）
// 适配器负责将领域模型翻译为外部支付商的 API 格式（ACL 边界）
type PaymentGateway interface {
	Authorize(ctx context.Context, token model.CardToken, amount model.Money) (*GatewayAuthResult, error)
	Capture(ctx context.Context, providerRef string, amount model.Money) error
	Refund(ctx context.Context, providerRef string, amount model.Money) error
}

// GatewayAuthResult 网关授权结果 — 接口绑定的 DTO，放在 port 中
type GatewayAuthResult struct {
	ProviderRef string
	AuthCode    string
}
