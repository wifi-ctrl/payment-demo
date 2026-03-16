// Package merchant 提供 payment 上下文对 merchant 上下文的 ACL 适配器。
// 跨上下文 import 仅允许在 adapter/ 层，此文件是唯一合法的跨上下文 import 位置。
package merchant

import (
	"context"

	merchantModel "payment-demo/internal/merchant/domain/model"
	merchantPort "payment-demo/internal/merchant/domain/port"
	"payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// MerchantAdapter ACL 适配器。
// 实现 payment 侧的 port.MerchantQuery，内部调用 merchant 上下文的 MerchantRepository，
// 将 merchant.ChannelCredential → payment.ChannelCredentialView（防腐翻译）。
type MerchantAdapter struct {
	repo merchantPort.MerchantRepository
}

// 编译期接口检查：确保 MerchantAdapter 实现 port.MerchantQuery。
var _ port.MerchantQuery = (*MerchantAdapter)(nil)

// NewMerchantAdapter 构造函数，注入 merchant 侧的仓储。
func NewMerchantAdapter(repo merchantPort.MerchantRepository) *MerchantAdapter {
	return &MerchantAdapter{repo: repo}
}

// FindActiveCredential 查询指定商户、指定渠道的有效凭据。
// 翻译链：
//   merchant.MerchantID (string) → merchantModel.MerchantID
//   model.PaymentMethod         → merchantModel.PaymentChannel（字面值对齐，直接转换）
//   merchant.ChannelCredential  → port.ChannelCredentialView
//
// 商户不存在、商户未激活、凭据不存在或已吊销，统一返回 port.ErrMerchantCredentialNotFound，
// 避免向 payment 层泄露 merchant 上下文的内部错误语义。
func (a *MerchantAdapter) FindActiveCredential(
	ctx context.Context,
	merchantID string,
	channel model.PaymentMethod,
) (*port.ChannelCredentialView, error) {
	// 1. 查询商户聚合根（merchant 侧仓储）
	m, err := a.repo.FindByID(ctx, merchantModel.MerchantID(merchantID))
	if err != nil {
		// merchant 不存在 → 统一映射为 payment 侧错误（防腐）
		return nil, port.ErrMerchantCredentialNotFound
	}

	// 2. 商户须处于 ACTIVE 状态，否则凭据不可用
	if m.Status != merchantModel.MerchantStatusActive {
		return nil, port.ErrMerchantCredentialNotFound
	}

	// 3. 查询指定渠道的 ACTIVE 凭据
	// PaymentMethod 与 PaymentChannel 字面值对齐（"CARD"/"PAYPAL"），直接类型转换
	cred, err := m.ActiveCredential(merchantModel.PaymentChannel(channel))
	if err != nil {
		return nil, port.ErrMerchantCredentialNotFound
	}

	// 4. ACL 翻译：merchant 实体 → payment 视图 DTO
	return &port.ChannelCredentialView{
		CredentialID: string(cred.ID),
		MerchantID:   merchantID,
		Channel:      string(cred.Channel),
		Secrets:      cred.Secrets,
	}, nil
}
