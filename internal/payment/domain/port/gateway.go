package port

import (
	"context"

	"payment-demo/internal/payment/domain/model"
)

// CaptureRefunder Capture/Refund 通用端口。
// PaymentGateway 和 PayPalGateway 隐式实现此接口。
// UseCase 通过 PaymentMethod 查表路由到对应实现，符合 OCP。
type CaptureRefunder interface {
	Capture(ctx context.Context, providerRef string, amount model.Money) error
	Refund(ctx context.Context, providerRef string, amount model.Money) error
}

// PaymentGateway 支付网关端口（被驱动端口）。
// 适配器负责将领域模型翻译为外部支付商的 API 格式（ACL 边界）。
type PaymentGateway interface {
	CaptureRefunder
	Authorize(ctx context.Context, token model.CardToken, amount model.Money) (*GatewayAuthResult, error)
}

// GatewayAuthResult 网关授权结果 — 接口绑定的 DTO，放在 port 中。
type GatewayAuthResult struct {
	ProviderRef string
	AuthCode    string
}

// GatewayFactory 根据商户渠道凭据动态构造 Gateway 实例（被驱动端口）。
// 适配器按 ChannelCredentialView.Secrets 初始化对应渠道的 HTTP 客户端，
// 实现多商户隔离：不同商户使用各自的 API Key / ClientID+Secret。
// 新增渠道只需在适配器层扩展，UseCase 无需修改（OCP）。
type GatewayFactory interface {
	// BuildCardGateway 为指定商户构建 Card 支付网关。
	// Secrets 中须包含 "api_key" 字段。
	BuildCardGateway(cred ChannelCredentialView) (PaymentGateway, error)

	// BuildPayPalGateway 为指定商户构建 PayPal 支付网关。
	// Secrets 中须包含 "client_id" 与 "client_secret" 字段。
	BuildPayPalGateway(cred ChannelCredentialView) (PayPalGateway, error)
}
