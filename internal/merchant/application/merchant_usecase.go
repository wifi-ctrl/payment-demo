// Package application 定义 merchant 上下文的用例编排层。
package application

import (
	"context"
	"log"

	"payment-demo/internal/merchant/domain/model"
	"payment-demo/internal/merchant/domain/port"
)

// MerchantUseCase 商户管理用例编排层。
// 依赖 port.MerchantRepository，通过构造函数注入；不依赖任何 adapter/infra 包。
type MerchantUseCase struct {
	repo port.MerchantRepository
}

// NewMerchantUseCase 构造函数注入仓储依赖。
func NewMerchantUseCase(repo port.MerchantRepository) *MerchantUseCase {
	return &MerchantUseCase{repo: repo}
}

// ─────────────────────────────────────────────────────────────────
// 注册商户
// ─────────────────────────────────────────────────────────────────

// RegisterRequest 注册商户请求。
type RegisterRequest struct {
	Name string
}

// Register 注册新商户：创建聚合根 → 持久化 → 发布事件。
func (uc *MerchantUseCase) Register(ctx context.Context, req RegisterRequest) (*model.Merchant, error) {
	m := model.NewMerchant(req.Name)
	if err := uc.repo.Save(ctx, m); err != nil {
		return nil, err
	}
	uc.publishEvents(m)
	return m, nil
}

// ─────────────────────────────────────────────────────────────────
// 添加渠道凭据
// ─────────────────────────────────────────────────────────────────

// AddCredentialRequest 添加渠道凭据请求。
// Secrets 格式因渠道而异：
//   - CARD:   {"api_key": "sk_live_xxx"}
//   - PAYPAL: {"client_id": "...", "client_secret": "..."}
type AddCredentialRequest struct {
	MerchantID string
	Channel    model.PaymentChannel
	Secrets    map[string]string
}

// AddCredential 为商户添加渠道凭据：查询 → 添加 → 持久化 → 发布事件。
func (uc *MerchantUseCase) AddCredential(ctx context.Context, req AddCredentialRequest) (*model.Merchant, error) {
	m, err := uc.repo.FindByID(ctx, model.MerchantID(req.MerchantID))
	if err != nil {
		return nil, err
	}
	if err := m.AddCredential(req.Channel, req.Secrets); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, m); err != nil {
		return nil, err
	}
	uc.publishEvents(m)
	return m, nil
}

// ─────────────────────────────────────────────────────────────────
// 吊销渠道凭据
// ─────────────────────────────────────────────────────────────────

// RevokeCredentialRequest 吊销凭据请求。
type RevokeCredentialRequest struct {
	MerchantID   string
	CredentialID string
}

// RevokeCredential 吊销指定渠道凭据：查询 → 吊销 → 持久化 → 发布事件。
func (uc *MerchantUseCase) RevokeCredential(ctx context.Context, req RevokeCredentialRequest) (*model.Merchant, error) {
	m, err := uc.repo.FindByID(ctx, model.MerchantID(req.MerchantID))
	if err != nil {
		return nil, err
	}
	if err := m.RevokeCredential(model.ChannelCredentialID(req.CredentialID)); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, m); err != nil {
		return nil, err
	}
	uc.publishEvents(m)
	return m, nil
}

// ─────────────────────────────────────────────────────────────────
// 暂停商户
// ─────────────────────────────────────────────────────────────────

// Suspend 暂停商户：查询 → 暂停 → 持久化 → 发布事件。
func (uc *MerchantUseCase) Suspend(ctx context.Context, merchantID string) (*model.Merchant, error) {
	m, err := uc.repo.FindByID(ctx, model.MerchantID(merchantID))
	if err != nil {
		return nil, err
	}
	if err := m.Suspend(); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, m); err != nil {
		return nil, err
	}
	uc.publishEvents(m)
	return m, nil
}

// ─────────────────────────────────────────────────────────────────
// 查询
// ─────────────────────────────────────────────────────────────────

// GetMerchant 按 ID 查询商户。
func (uc *MerchantUseCase) GetMerchant(ctx context.Context, merchantID string) (*model.Merchant, error) {
	return uc.repo.FindByID(ctx, model.MerchantID(merchantID))
}

// ListMerchants 列出所有商户（管理后台用）。
func (uc *MerchantUseCase) ListMerchants(ctx context.Context) ([]*model.Merchant, error) {
	return uc.repo.FindAll(ctx)
}

// ─────────────────────────────────────────────────────────────────
// 内部辅助
// ─────────────────────────────────────────────────────────────────

func (uc *MerchantUseCase) publishEvents(m *model.Merchant) {
	for _, evt := range m.ClearEvents() {
		log.Printf("[DomainEvent] %s: %+v", evt.EventName(), evt)
	}
}
