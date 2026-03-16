package application

import (
	"context"
	"log"

	"payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
	"payment-demo/internal/payment/domain/service"
)

// ChargeUseCase 支付用例编排层。
//
// 多商户路由策略：
//   Purchase / PayPalPurchase 入参携带 MerchantID，
//   UseCase 先通过 MerchantQuery ACL 获取该商户对应渠道的凭据（ChannelCredentialView），
//   再通过 GatewayFactory 按凭据动态构造 Gateway 实例，实现商户级隔离。
//
// 字段说明：
//   merchantQuery:    ACL 端口，查询商户渠道凭据（由 adapter/merchant 实现）
//   gatewayFactory:   动态构造 Gateway（由 adapter/gateway 实现，按商户凭据初始化 HTTP 客户端）
//   captureRefunders: Capture/Refund 按 PaymentMethod 查表路由，新增支付方式只需注册（OCP）
//   repo:             交易仓储
//   catalog:          商品 ACL 查询端口
//   cardQuery:        已保存卡 ACL 查询端口
type ChargeUseCase struct {
	merchantQuery    port.MerchantQuery
	gatewayFactory   port.GatewayFactory
	captureRefunders map[model.PaymentMethod]port.CaptureRefunder
	repo             port.TransactionRepository
	catalog          port.CatalogQuery
	cardQuery        port.CardQuery
}

// NewChargeUseCase 构造函数注入所有依赖。
// captureRefunders 在此处初始化为空 map，Purchase/PayPalPurchase 执行时按商户凭据动态构建；
// Capture/Refund 需要在调用时从仓储中恢复交易，再按 txn.Method 路由 —— 见下方说明。
//
// 注意：多商户场景下，Capture/Refund 须通过 GatewayFactory + 存储的凭据重建 Gateway。
// 为保持 Demo 简洁，此处 captureRefunders 仍保留静态路由表，
// 由 NewChargeUseCase 调用方在注入时传入"默认"Gateway（或可替换为按交易 MerchantID 动态查找）。
// 若需完整多商户 Capture/Refund，可在交易聚合根中冗余存储 MerchantID，此处已在 transaction 扩展字段中预留。
func NewChargeUseCase(
	merchantQuery port.MerchantQuery,
	gatewayFactory port.GatewayFactory,
	repo port.TransactionRepository,
	catalog port.CatalogQuery,
	cardQuery port.CardQuery,
) *ChargeUseCase {
	return &ChargeUseCase{
		merchantQuery:    merchantQuery,
		gatewayFactory:   gatewayFactory,
		captureRefunders: make(map[model.PaymentMethod]port.CaptureRefunder),
		repo:             repo,
		catalog:          catalog,
		cardQuery:        cardQuery,
	}
}

// ─────────────────────────────────────────────────────────────────
// Card 购买（多商户版）
// ─────────────────────────────────────────────────────────────────

// PurchaseRequest 用例层入参。
// MerchantID 必填，标识本次交易归属商户，用于路由渠道凭据。
// SavedCardID 与 Token 二选一：
//   - 传 SavedCardID：通过 CardQuery ACL 查询已保存卡，获取 VaultToken 构造 CardToken
//   - 传 Token：直接使用前端一次性 Token（原有流程）
type PurchaseRequest struct {
	MerchantID  string
	UserID      string
	ProductID   string
	Token       model.CardToken // 一次性 Token（与 SavedCardID 二选一）
	SavedCardID string          // 已保存卡 ID（与 Token 二选一，非空时优先使用）
}

// Purchase 卡支付购买用例：验证商户凭据 → 验证商品 → 解析卡信息 → 按商户凭据构建 Gateway → 授权 → 持久化。
func (uc *ChargeUseCase) Purchase(ctx context.Context, req PurchaseRequest) (*model.PaymentTransaction, error) {
	// 1. 校验 MerchantID
	if req.MerchantID == "" {
		return nil, model.ErrMerchantRequired
	}

	// 2. 查商品（CatalogQuery ACL，不直接依赖 catalog 上下文）
	product, err := uc.catalog.FindProduct(ctx, req.ProductID)
	if err != nil {
		return nil, err
	}
	if !product.IsActive {
		return nil, model.ErrProductNotActive
	}

	log.Printf("[UseCase] Purchase: merchant=%s, user=%s, product=%s, price=%d %s",
		req.MerchantID, req.UserID, product.Name, product.Amount, product.Currency)

	// 3. 解析支付卡：SavedCardID 非空时通过 ACL 查询已保存卡
	cardToken := req.Token
	if req.SavedCardID != "" {
		cardView, err := uc.cardQuery.FindActiveCard(ctx, req.SavedCardID)
		if err != nil {
			return nil, err
		}
		if cardView.UserID != req.UserID {
			return nil, model.ErrCardNotFound
		}
		if !cardView.IsActive {
			return nil, model.ErrCardNotUsable
		}
		cardToken = model.CardToken{
			TokenID: cardView.Token,
			Last4:   cardView.Last4,
			Brand:   cardView.Brand,
		}
	}

	// 4. 通过 MerchantQuery ACL 获取该商户 CARD 渠道的凭据
	cred, err := uc.merchantQuery.FindActiveCredential(ctx, req.MerchantID, model.PaymentMethodCard)
	if err != nil {
		return nil, err // port.ErrMerchantCredentialNotFound
	}

	// 5. 按商户凭据动态构建 Card Gateway（商户级隔离）
	gateway, err := uc.gatewayFactory.BuildCardGateway(*cred)
	if err != nil {
		return nil, model.ErrMerchantGatewayBuildFailed
	}

	// 6. 创建交易
	amount := model.NewMoney(product.Amount, product.Currency)
	txn := model.NewPaymentTransaction(req.UserID, product.ID, amount, cardToken)
	txn.MerchantID = req.MerchantID // 冗余存储，便于 Capture/Refund 时重建 Gateway

	// 7. 调商户专属 Card Gateway 授权
	result, err := gateway.Authorize(ctx, cardToken, amount)
	if err != nil {
		txn.MarkFailed(err.Error())
		_ = uc.repo.Save(ctx, txn)
		return txn, model.ErrAuthorizationDeclined
	}

	// 8. 授权成功：状态转换 → 持久化 → 发布事件
	if err := txn.MarkAuthorized(result.ProviderRef, result.AuthCode); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, txn); err != nil {
		return nil, err
	}

	uc.publishEvents(txn)
	return txn, nil
}

// ─────────────────────────────────────────────────────────────────
// PayPal 购买（多商户版）
// ─────────────────────────────────────────────────────────────────

// PayPalPurchaseRequest PayPal 购买用例入参。
// MerchantID 必填。
type PayPalPurchaseRequest struct {
	MerchantID string
	UserID     string
	ProductID  string
	Token      model.PayPalToken // 前端 JS SDK 返回的 OrderID + PayerID
}

// PayPalPurchase PayPal 购买用例：验证商户凭据 → 查商品 → 按商户凭据构建 PayPal Gateway → 授权 → 持久化。
func (uc *ChargeUseCase) PayPalPurchase(ctx context.Context, req PayPalPurchaseRequest) (*model.PaymentTransaction, error) {
	// 1. 校验 MerchantID
	if req.MerchantID == "" {
		return nil, model.ErrMerchantRequired
	}

	// 2. 查商品
	product, err := uc.catalog.FindProduct(ctx, req.ProductID)
	if err != nil {
		return nil, err
	}
	if !product.IsActive {
		return nil, model.ErrProductNotActive
	}

	log.Printf("[UseCase] PayPalPurchase: merchant=%s, user=%s, product=%s, price=%d %s, orderID=%s",
		req.MerchantID, req.UserID, product.Name, product.Amount, product.Currency, req.Token.OrderID)

	// 3. 通过 MerchantQuery ACL 获取该商户 PAYPAL 渠道的凭据
	cred, err := uc.merchantQuery.FindActiveCredential(ctx, req.MerchantID, model.PaymentMethodPayPal)
	if err != nil {
		return nil, err
	}

	// 4. 按商户凭据动态构建 PayPal Gateway
	paypalGateway, err := uc.gatewayFactory.BuildPayPalGateway(*cred)
	if err != nil {
		return nil, model.ErrMerchantGatewayBuildFailed
	}

	amount := model.NewMoney(product.Amount, product.Currency)

	// 5. 创建 PayPal 交易
	txn := model.NewPayPalTransaction(req.UserID, product.ID, amount, req.Token)
	txn.MerchantID = req.MerchantID

	// 6. 调商户专属 PayPal Gateway 授权
	result, err := paypalGateway.Authorize(ctx, req.Token, amount)
	if err != nil {
		txn.MarkFailed(err.Error())
		_ = uc.repo.Save(ctx, txn)
		return txn, model.ErrAuthorizationDeclined
	}

	// 7. 授权成功
	if err := txn.MarkAuthorized(result.ProviderRef, ""); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, txn); err != nil {
		return nil, err
	}

	uc.publishEvents(txn)
	return txn, nil
}

// ─────────────────────────────────────────────────────────────────
// Capture / Refund（多商户版：按存储的 MerchantID 重建 Gateway）
// ─────────────────────────────────────────────────────────────────

// Capture 扣款用例：恢复交易 → 按 MerchantID+Method 重建 Gateway → 扣款 → 状态转换 → 持久化。
func (uc *ChargeUseCase) Capture(ctx context.Context, txnID model.TransactionID) (*model.PaymentTransaction, error) {
	txn, err := uc.repo.FindByID(ctx, txnID)
	if err != nil {
		return nil, err
	}
	if err := service.ValidateCapturable(txn); err != nil {
		return nil, err
	}

	gw, err := uc.buildCaptureRefunder(ctx, txn)
	if err != nil {
		return nil, err
	}

	if err := gw.Capture(ctx, txn.ProviderRef, txn.Amount); err != nil {
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

// Refund 退款用例：恢复交易 → 按 MerchantID+Method 重建 Gateway → 退款 → 状态转换 → 持久化。
func (uc *ChargeUseCase) Refund(ctx context.Context, txnID model.TransactionID) (*model.PaymentTransaction, error) {
	txn, err := uc.repo.FindByID(ctx, txnID)
	if err != nil {
		return nil, err
	}
	if err := service.ValidateRefundable(txn); err != nil {
		return nil, err
	}

	gw, err := uc.buildCaptureRefunder(ctx, txn)
	if err != nil {
		return nil, err
	}

	if err := gw.Refund(ctx, txn.ProviderRef, txn.Amount); err != nil {
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

// buildCaptureRefunder 按交易的 MerchantID + Method 动态重建 Gateway（CaptureRefunder 接口）。
// 这是多商户 Capture/Refund 的核心路由逻辑：
//   - 从交易中读取冗余的 MerchantID（由 Purchase 时写入）
//   - 通过 MerchantQuery 重新获取当前有效凭据
//   - 通过 GatewayFactory 构建对应渠道的 Gateway
func (uc *ChargeUseCase) buildCaptureRefunder(ctx context.Context, txn *model.PaymentTransaction) (port.CaptureRefunder, error) {
	if txn.MerchantID == "" {
		return nil, model.ErrMerchantRequired
	}

	cred, err := uc.merchantQuery.FindActiveCredential(ctx, txn.MerchantID, txn.Method)
	if err != nil {
		return nil, err
	}

	switch txn.Method {
	case model.PaymentMethodCard:
		return uc.gatewayFactory.BuildCardGateway(*cred)
	case model.PaymentMethodPayPal:
		return uc.gatewayFactory.BuildPayPalGateway(*cred)
	default:
		return nil, model.ErrUnsupportedPaymentMethod
	}
}

// ─────────────────────────────────────────────────────────────────
// 查询（不变）
// ─────────────────────────────────────────────────────────────────

// GetTransaction 查询交易用例。
func (uc *ChargeUseCase) GetTransaction(ctx context.Context, txnID model.TransactionID) (*model.PaymentTransaction, error) {
	return uc.repo.FindByID(ctx, txnID)
}

func (uc *ChargeUseCase) publishEvents(txn *model.PaymentTransaction) {
	for _, evt := range txn.ClearEvents() {
		log.Printf("[DomainEvent] %s: %+v", evt.EventName(), evt)
	}
}
