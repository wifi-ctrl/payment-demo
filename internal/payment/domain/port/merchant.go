package port

import (
	"context"
	"errors"

	"payment-demo/internal/payment/domain/model"
)

// ErrMerchantCredentialNotFound payment 上下文中商户凭据不存在或已吊销时返回。
var ErrMerchantCredentialNotFound = errors.New("merchant channel credential not found or revoked")

// MerchantQuery payment 上下文对 merchant 上下文的 ACL 查询端口。
// 消费方（payment）定义，adapter/merchant/ 提供实现。
// ChargeUseCase 通过此端口获取商户渠道凭据，决定路由哪个 Gateway 实例。
type MerchantQuery interface {
	// FindActiveCredential 查询指定商户、指定渠道的有效凭据。
	// 凭据不存在或已吊销时返回 ErrMerchantCredentialNotFound。
	FindActiveCredential(ctx context.Context, merchantID string, channel model.PaymentMethod) (*ChannelCredentialView, error)
}

// ChannelCredentialView payment 视角的渠道凭据视图（接口绑定 DTO）。
// 只暴露 Gateway 初始化所需的最小字段，隐藏 merchant 侧的实体细节。
type ChannelCredentialView struct {
	CredentialID string
	MerchantID   string
	Channel      string            // "CARD" / "PAYPAL"，与 PaymentMethod 字面值对齐
	Secrets      map[string]string // 传递给 GatewayFactory 用于构造渠道专属 HTTP 客户端
}
