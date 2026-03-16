package application

import (
	"context"
	"fmt"
	"log"

	"payment-demo/internal/card/domain/model"
	"payment-demo/internal/card/domain/port"
)

// CardUseCase 卡管理用例编排层
type CardUseCase struct {
	repo  port.CardRepository
	vault port.CardVault
}

func NewCardUseCase(repo port.CardRepository, vault port.CardVault) *CardUseCase {
	return &CardUseCase{repo: repo, vault: vault}
}

// BindCardRequest 绑卡用例入参
type BindCardRequest struct {
	UserID       string
	OneTimeToken string // 前端 tokenization 生成的一次性 Token
}

// BindCard 绑卡用例：调 Vault 换取持久令牌 → 创建聚合根 → 判断是否首张卡 → 持久化
func (uc *CardUseCase) BindCard(ctx context.Context, req BindCardRequest) (*model.SavedCard, error) {
	// 1. 调 Vault Gateway 换取持久令牌 + 脱敏信息
	result, err := uc.vault.Tokenize(ctx, req.OneTimeToken)
	if err != nil {
		// W-7: 用 %w 包装 sentinel，保留原始错误链供调试
		return nil, fmt.Errorf("%w: %v", model.ErrVaultTokenizeFailed, err)
	}

	// 2. 创建聚合根
	card := model.NewSavedCard(req.UserID, result.VaultToken, result.Mask, result.Holder)

	// 3. 判断用户是否已有卡：无则设为默认卡
	existing, err := uc.repo.FindDefaultByUserID(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		card.BindAsDefault()
	} else {
		card.Bind()
	}

	// 4. 持久化
	if err := uc.repo.Save(ctx, card); err != nil {
		return nil, err
	}

	uc.publishEvents(card)
	return card, nil
}

// ListCards 查询用户全部未删除的卡
func (uc *CardUseCase) ListCards(ctx context.Context, userID string) ([]*model.SavedCard, error) {
	return uc.repo.FindAllByUserID(ctx, userID)
}

// SuspendCard 挂起卡（Active → Suspended）
func (uc *CardUseCase) SuspendCard(ctx context.Context, userID string, cardID model.SavedCardID) (*model.SavedCard, error) {
	card, err := uc.findOwnedCard(ctx, userID, cardID)
	if err != nil {
		return nil, err
	}

	if err := card.Suspend(); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, card); err != nil {
		return nil, err
	}

	uc.publishEvents(card)
	return card, nil
}

// ActivateCard 解除挂起（Suspended → Active）
func (uc *CardUseCase) ActivateCard(ctx context.Context, userID string, cardID model.SavedCardID) (*model.SavedCard, error) {
	card, err := uc.findOwnedCard(ctx, userID, cardID)
	if err != nil {
		return nil, err
	}

	if err := card.Activate(); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, card); err != nil {
		return nil, err
	}

	// W-1: Activate 现已定义 CardActivated 事件，统一发布
	uc.publishEvents(card)
	return card, nil
}

// DeleteCard 删除卡：软删除聚合根 → 调 Vault 删除 Token
func (uc *CardUseCase) DeleteCard(ctx context.Context, userID string, cardID model.SavedCardID) error {
	card, err := uc.findOwnedCard(ctx, userID, cardID)
	if err != nil {
		return err
	}

	if err := card.Delete(); err != nil {
		return err
	}
	if err := uc.repo.Save(ctx, card); err != nil {
		return err
	}

	// 通知 Vault 删除令牌，防止泄露（best-effort，失败仅记录日志）
	if err := uc.vault.Delete(ctx, card.VaultToken); err != nil {
		log.Printf("[CardUseCase] Vault.Delete failed: cardID=%s, err=%v", cardID, err)
	}

	uc.publishEvents(card)
	return nil
}

// SetDefaultCard 切换默认卡：先 UnsetDefault 旧卡 → SetDefault 新卡 → 持久化两张
// W-5: 优先持久化新卡，再持久化旧卡，降低不一致窗口；新卡 Save 失败时直接返回错误。
func (uc *CardUseCase) SetDefaultCard(ctx context.Context, userID string, cardID model.SavedCardID) (*model.SavedCard, error) {
	// 1. 查目标卡
	card, err := uc.findOwnedCard(ctx, userID, cardID)
	if err != nil {
		return nil, err
	}

	// 2. 查当前默认卡（可能为 nil）
	oldDefault, err := uc.repo.FindDefaultByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 3. 先完成所有状态变更（内存层面）
	if oldDefault != nil && oldDefault.ID != cardID {
		oldDefault.UnsetDefault()
	}
	if err := card.SetDefault(); err != nil {
		return nil, err
	}

	// 4. 新卡优先持久化：新卡 Save 成功后再保存旧卡
	if err := uc.repo.Save(ctx, card); err != nil {
		return nil, err
	}
	if oldDefault != nil && oldDefault.ID != cardID {
		if err := uc.repo.Save(ctx, oldDefault); err != nil {
			// 新卡已保存成功，旧卡取消默认失败：记录告警，业务可通过运维修复
			log.Printf("[CardUseCase] SetDefaultCard: new card saved but old card unset failed: %v", err)
		}
	}

	uc.publishEvents(card)
	return card, nil
}

// GetCard 查询单张卡详情
func (uc *CardUseCase) GetCard(ctx context.Context, userID string, cardID model.SavedCardID) (*model.SavedCard, error) {
	return uc.findOwnedCard(ctx, userID, cardID)
}

// findOwnedCard 查询并校验卡归属
func (uc *CardUseCase) findOwnedCard(ctx context.Context, userID string, cardID model.SavedCardID) (*model.SavedCard, error) {
	card, err := uc.repo.FindByID(ctx, cardID)
	if err != nil {
		return nil, err
	}
	if card.UserID != userID {
		return nil, model.ErrCardBelongsToOtherUser
	}
	return card, nil
}

func (uc *CardUseCase) publishEvents(card *model.SavedCard) {
	for _, evt := range card.ClearEvents() {
		log.Printf("[DomainEvent] %s: %+v", evt.EventName(), evt)
	}
}
