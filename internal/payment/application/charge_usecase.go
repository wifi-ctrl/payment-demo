package application

import (
	"context"
	"log"

	"payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
	"payment-demo/internal/payment/domain/service"
)

// ChargeUseCase 支付用例编排层
type ChargeUseCase struct {
	gateway   port.PaymentGateway
	repo      port.TransactionRepository
	catalog   port.CatalogQuery
	cardQuery port.CardQuery // ACL — 查询 card 上下文的已保存卡
}

func NewChargeUseCase(
	gateway port.PaymentGateway,
	repo port.TransactionRepository,
	catalog port.CatalogQuery,
	cardQuery port.CardQuery,
) *ChargeUseCase {
	return &ChargeUseCase{
		gateway:   gateway,
		repo:      repo,
		catalog:   catalog,
		cardQuery: cardQuery,
	}
}

// PurchaseRequest 用例层入参
// SavedCardID 与 Token 二选一：
//   - 传 SavedCardID：通过 CardQuery ACL 查询已保存卡，获取 VaultToken 构造 CardToken
//   - 传 Token：直接使用前端一次性 Token（原有流程）
type PurchaseRequest struct {
	UserID      string
	ProductID   string
	Token       model.CardToken // 一次性 Token（与 SavedCardID 二选一）
	SavedCardID string          // 已保存卡 ID（与 Token 二选一，非空时优先使用）
}

// Purchase 购买用例：验证商品 → 解析卡信息 → 授权 → 持久化
func (uc *ChargeUseCase) Purchase(ctx context.Context, req PurchaseRequest) (*model.PaymentTransaction, error) {
	// 1. 查商品（通过 payment 自己的 CatalogQuery 端口，不直接依赖 catalog 上下文）
	product, err := uc.catalog.FindProduct(ctx, req.ProductID)
	if err != nil {
		return nil, err
	}
	if !product.IsActive {
		return nil, model.ErrProductNotActive
	}

	log.Printf("[UseCase] Purchase: user=%s, product=%s, price=%d %s",
		req.UserID, product.Name, product.Amount, product.Currency)

	// 2. 解析支付卡：SavedCardID 非空时通过 ACL 查询已保存卡
	cardToken := req.Token
	if req.SavedCardID != "" {
		cardView, err := uc.cardQuery.FindActiveCard(ctx, req.SavedCardID)
		if err != nil {
			return nil, err
		}
		// 校验卡归属用户
		if cardView.UserID != req.UserID {
			return nil, model.ErrCardNotFound
		}
		if !cardView.IsActive {
			return nil, model.ErrCardNotUsable
		}
		// 用 VaultToken 构造 CardToken，复用现有 Authorize 流程
		cardToken = model.CardToken{
			TokenID: cardView.Token,
			Last4:   cardView.Last4,
			Brand:   cardView.Brand,
		}
	}

	// 3. 创建交易
	amount := model.NewMoney(product.Amount, product.Currency)
	txn := model.NewPaymentTransaction(req.UserID, product.ID, amount, cardToken)

	// 4. 调支付网关
	result, err := uc.gateway.Authorize(ctx, cardToken, amount)
	if err != nil {
		txn.MarkFailed(err.Error())
		_ = uc.repo.Save(ctx, txn)
		return txn, model.ErrAuthorizationDeclined
	}

	// 5. 授权成功
	if err := txn.MarkAuthorized(result.ProviderRef, result.AuthCode); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, txn); err != nil {
		return nil, err
	}

	uc.publishEvents(txn)
	return txn, nil
}

// Capture 扣款用例
func (uc *ChargeUseCase) Capture(ctx context.Context, txnID model.TransactionID) (*model.PaymentTransaction, error) {
	txn, err := uc.repo.FindByID(ctx, txnID)
	if err != nil {
		return nil, err
	}
	if err := service.ValidateCapturable(txn); err != nil {
		return nil, err
	}
	if err := uc.gateway.Capture(ctx, txn.ProviderRef, txn.Amount); err != nil {
		return nil, model.ErrCaptureFailure
	}
	if err := txn.MarkCaptured(); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, txn); err != nil {
		return nil, err
	}

	uc.publishEvents(txn)
	return txn, nil
}

// Refund 退款用例
func (uc *ChargeUseCase) Refund(ctx context.Context, txnID model.TransactionID) (*model.PaymentTransaction, error) {
	txn, err := uc.repo.FindByID(ctx, txnID)
	if err != nil {
		return nil, err
	}
	if err := service.ValidateRefundable(txn); err != nil {
		return nil, err
	}
	if err := uc.gateway.Refund(ctx, txn.ProviderRef, txn.Amount); err != nil {
		return nil, model.ErrRefundFailure
	}
	if err := txn.MarkRefunded(); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, txn); err != nil {
		return nil, err
	}

	uc.publishEvents(txn)
	return txn, nil
}

// GetTransaction 查询用例
func (uc *ChargeUseCase) GetTransaction(ctx context.Context, txnID model.TransactionID) (*model.PaymentTransaction, error) {
	return uc.repo.FindByID(ctx, txnID)
}

func (uc *ChargeUseCase) publishEvents(txn *model.PaymentTransaction) {
	for _, evt := range txn.ClearEvents() {
		log.Printf("[DomainEvent] %s: %+v", evt.EventName(), evt)
	}
}
