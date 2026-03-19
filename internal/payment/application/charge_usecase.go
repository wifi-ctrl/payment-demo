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
//
//	Purchase / PayPalPurchase 入参携带 MerchantID，
//	UseCase 先通过 MerchantQuery ACL 获取该商户对应渠道的凭据（ChannelCredentialView），
//	再通过 GatewayFactory 按凭据动态构造 Gateway 实例，实现商户级隔离。
//
// 定价计算是 Purchase 流程的固有步骤：
//
//	1. 查商品原价
//	2. 如有 CouponCode → CouponApplier.Apply → 获取折扣信息
//	3. 查税率（TaxRateQuery）
//	4. 纯计算：原价 - 折扣 + 税
//	5. 用 FinalAmount 创建交易
//	6. 授权失败时 → CouponApplier.Rollback（Saga 补偿）
type ChargeUseCase struct {
	merchantQuery    port.MerchantQuery
	gatewayFactory   port.GatewayFactory
	captureRefunders map[model.PaymentMethod]port.CaptureRefunder
	repo             port.TransactionRepository
	catalog          port.CatalogQuery
	cardQuery        port.CardQuery
	cardCommand      port.CardCommand   // 支付成功后回存 token / 绑卡
	couponApplier    port.CouponApplier // nil = 无优惠券支持
	taxQuery         port.TaxRateQuery  // nil = 免税
}

// NewChargeUseCase 构造函数注入所有依赖。
// couponApplier / taxQuery 可传 nil（无优惠券/免税场景）。
func NewChargeUseCase(
	merchantQuery port.MerchantQuery,
	gatewayFactory port.GatewayFactory,
	repo port.TransactionRepository,
	catalog port.CatalogQuery,
	cardQuery port.CardQuery,
	cardCommand port.CardCommand,
	couponApplier port.CouponApplier,
	taxQuery port.TaxRateQuery,
) *ChargeUseCase {
	return &ChargeUseCase{
		merchantQuery:    merchantQuery,
		gatewayFactory:   gatewayFactory,
		captureRefunders: make(map[model.PaymentMethod]port.CaptureRefunder),
		repo:             repo,
		catalog:          catalog,
		cardQuery:        cardQuery,
		cardCommand:      cardCommand,
		couponApplier:    couponApplier,
		taxQuery:         taxQuery,
	}
}

// ─────────────────────────────────────────────────────────────────
// Card 购买
// ─────────────────────────────────────────────────────────────────

// PurchaseRequest 用例层入参。
type PurchaseRequest struct {
	MerchantID  string
	UserID      string
	ProductID   string
	Token       model.CardToken // 一次性 Token（与 SavedCardID 二选一）
	SavedCardID string          // 已保存卡 ID（与 Token 二选一，非空时优先使用）
	SaveCard    bool            // 首次支付后是否绑卡（仅 Token 模式有效）
	CouponCode  string          // 可选，非空时使用优惠券
}

// Purchase 卡支付购买用例。
//
// 复购路径（SavedCardID 非空）：
//  1. 查已保存卡 → 匹配 ChannelToken[targetChannel]
//  2. 有 ChannelToken → 直接用 recurring token 授权
//  3. 无 ChannelToken → PrepareOneTimeToken 解密 PAN → 用一次性 token 授权
//  4. 授权成功后 → StoreChannelToken 回存复购 token
//
// 首次路径（Token 非空）：
//  1. 用一次性 token 授权
//  2. SaveCard=true + 授权返回 RecurringToken → BindCardFromToken 绑卡
func (uc *ChargeUseCase) Purchase(ctx context.Context, req PurchaseRequest) (*model.PaymentTransaction, error) {
	if req.MerchantID == "" {
		return nil, model.ErrMerchantRequired
	}

	// 1. 查商品
	product, err := uc.catalog.FindProduct(ctx, req.ProductID)
	if err != nil {
		return nil, err
	}
	if !product.IsActive {
		return nil, model.ErrProductNotActive
	}

	log.Printf("[UseCase] Purchase: merchant=%s, user=%s, product=%s, price=%d %s",
		req.MerchantID, req.UserID, product.Name, product.Amount, product.Currency)

	// 2. 获取商户 CARD 渠道凭据
	cred, err := uc.merchantQuery.FindActiveCredential(ctx, req.MerchantID, model.PaymentMethodCard)
	if err != nil {
		return nil, err
	}
	targetChannel := cred.Channel

	// 3. 构建 Card Gateway
	gateway, err := uc.gatewayFactory.BuildCardGateway(*cred)
	if err != nil {
		return nil, model.ErrMerchantGatewayBuildFailed
	}

	// 4. 解析支付卡 token
	cardToken := req.Token
	var savedCardID string
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
		savedCardID = cardView.CardID

		// 匹配 ChannelToken：优先使用渠道复购 token
		if recurringToken, ok := cardView.ChannelTokens[targetChannel]; ok {
			cardToken = model.CardToken{
				TokenID: recurringToken,
				Last4:   cardView.Last4,
				Brand:   cardView.Brand,
			}
			log.Printf("[UseCase] Using recurring token for channel=%s", targetChannel)
		} else {
			// 无 ChannelToken → PrepareOneTimeToken 降级
			oneTimeToken, err := uc.cardCommand.PrepareOneTimeToken(ctx, cardView.CardID, req.UserID)
			if err != nil {
				return nil, err
			}
			cardToken = model.CardToken{
				TokenID: oneTimeToken,
				Last4:   cardView.Last4,
				Brand:   cardView.Brand,
			}
			log.Printf("[UseCase] No recurring token for channel=%s, using one-time token", targetChannel)
		}
	}

	// 5. 应用优惠券（如有）
	original := model.NewMoney(product.Amount, product.Currency)
	var coupon *port.AppliedCoupon
	var discountType string
	var discountValue int64

	if req.CouponCode != "" && uc.couponApplier != nil {
		coupon, err = uc.couponApplier.Apply(ctx, req.CouponCode, req.UserID)
		if err != nil {
			return nil, err
		}
		discountType = coupon.DiscountType
		discountValue = coupon.DiscountValue
	}

	// 6. 查税率
	taxBP := uc.queryTaxRate(ctx, req.ProductID, product.Currency)

	// 7. 计算最终金额
	finalAmount, discountAmount, taxAmount, err := service.CalculateFinalAmount(original, discountType, discountValue, taxBP)
	if err != nil {
		uc.rollbackCoupon(ctx, req.CouponCode)
		return nil, err
	}

	// 8. 创建交易
	amount := model.NewMoney(finalAmount.Amount, finalAmount.Currency)
	txn := model.NewPaymentTransaction(req.UserID, product.ID, amount, cardToken)
	txn.MerchantID = req.MerchantID
	uc.attachAudit(txn, coupon, discountAmount, taxAmount)

	// 9. 授权
	result, err := gateway.Authorize(ctx, cardToken, amount)
	if err != nil {
		uc.rollbackCoupon(ctx, req.CouponCode)
		txn.MarkFailed(err.Error())
		if saveErr := uc.repo.Save(ctx, txn); saveErr != nil {
			log.Printf("[UseCase] failed to save failed txn: %v", saveErr)
		}
		return txn, model.ErrAuthorizationDeclined
	}

	// 10. 持久化
	if err := txn.MarkAuthorized(result.ProviderRef, result.AuthCode); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, txn); err != nil {
		uc.rollbackCoupon(ctx, req.CouponCode)
		return nil, err
	}

	// 11. 授权成功后：回存 ChannelToken / 绑卡
	uc.postAuthTokenStorage(ctx, result, req, savedCardID, targetChannel)

	uc.publishEvents(txn)
	return txn, nil
}

// ─────────────────────────────────────────────────────────────────
// PayPal 购买
// ─────────────────────────────────────────────────────────────────

// PayPalPurchaseRequest PayPal 购买用例入参。
type PayPalPurchaseRequest struct {
	MerchantID string
	UserID     string
	ProductID  string
	Token      model.PayPalToken
	CouponCode string
}

// PayPalPurchase PayPal 购买用例。
func (uc *ChargeUseCase) PayPalPurchase(ctx context.Context, req PayPalPurchaseRequest) (*model.PaymentTransaction, error) {
	if req.MerchantID == "" {
		return nil, model.ErrMerchantRequired
	}

	product, err := uc.catalog.FindProduct(ctx, req.ProductID)
	if err != nil {
		return nil, err
	}
	if !product.IsActive {
		return nil, model.ErrProductNotActive
	}

	log.Printf("[UseCase] PayPalPurchase: merchant=%s, user=%s, product=%s, price=%d %s, orderID=%s",
		req.MerchantID, req.UserID, product.Name, product.Amount, product.Currency, req.Token.OrderID)

	cred, err := uc.merchantQuery.FindActiveCredential(ctx, req.MerchantID, model.PaymentMethodPayPal)
	if err != nil {
		return nil, err
	}

	paypalGateway, err := uc.gatewayFactory.BuildPayPalGateway(*cred)
	if err != nil {
		return nil, model.ErrMerchantGatewayBuildFailed
	}

	// 优惠券 + 税率 + 计算
	original := model.NewMoney(product.Amount, product.Currency)
	var coupon *port.AppliedCoupon
	var discountType string
	var discountValue int64

	if req.CouponCode != "" && uc.couponApplier != nil {
		coupon, err = uc.couponApplier.Apply(ctx, req.CouponCode, req.UserID)
		if err != nil {
			return nil, err
		}
		discountType = coupon.DiscountType
		discountValue = coupon.DiscountValue
	}

	taxBP := uc.queryTaxRate(ctx, req.ProductID, product.Currency)

	finalAmount, discountAmount, taxAmount, err := service.CalculateFinalAmount(original, discountType, discountValue, taxBP)
	if err != nil {
		uc.rollbackCoupon(ctx, req.CouponCode)
		return nil, err
	}

	amount := model.NewMoney(finalAmount.Amount, finalAmount.Currency)
	txn := model.NewPayPalTransaction(req.UserID, product.ID, amount, req.Token)
	txn.MerchantID = req.MerchantID
	uc.attachAudit(txn, coupon, discountAmount, taxAmount)

	result, err := paypalGateway.Authorize(ctx, req.Token, amount)
	if err != nil {
		uc.rollbackCoupon(ctx, req.CouponCode)
		txn.MarkFailed(err.Error())
		_ = uc.repo.Save(ctx, txn)
		return txn, model.ErrAuthorizationDeclined
	}

	if err := txn.MarkAuthorized(result.ProviderRef, ""); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, txn); err != nil {
		uc.rollbackCoupon(ctx, req.CouponCode)
		return nil, err
	}

	uc.publishEvents(txn)
	return txn, nil
}

// ─────────────────────────────────────────────────────────────────
// Capture / Refund
// ─────────────────────────────────────────────────────────────────

// Capture 扣款用例。
func (uc *ChargeUseCase) Capture(ctx context.Context, txnID model.TransactionID) (*model.PaymentTransaction, error) {
	txn, err := uc.repo.FindByID(ctx, txnID)
	if err != nil {
		return nil, err
	}
	if err := txn.ValidateCapturable(); err != nil {
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

// Refund 退款用例。
func (uc *ChargeUseCase) Refund(ctx context.Context, txnID model.TransactionID) (*model.PaymentTransaction, error) {
	txn, err := uc.repo.FindByID(ctx, txnID)
	if err != nil {
		return nil, err
	}
	if err := txn.ValidateRefundable(); err != nil {
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
// 查询
// ─────────────────────────────────────────────────────────────────

// GetTransaction 查询交易用例。
func (uc *ChargeUseCase) GetTransaction(ctx context.Context, txnID model.TransactionID) (*model.PaymentTransaction, error) {
	return uc.repo.FindByID(ctx, txnID)
}

// ─────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────

func (uc *ChargeUseCase) queryTaxRate(ctx context.Context, productID, currency string) int64 {
	if uc.taxQuery == nil {
		return 0
	}
	bp, err := uc.taxQuery.FindTaxRate(ctx, productID, currency)
	if err != nil {
		log.Printf("[UseCase] TaxRateQuery failed (productID=%s, currency=%s): %v — using 0", productID, currency, err)
		return 0
	}
	return bp
}

func (uc *ChargeUseCase) attachAudit(txn *model.PaymentTransaction, coupon *port.AppliedCoupon, discount, tax model.Money) {
	if !discount.IsZero() {
		txn.DiscountAmount = &discount
	}
	if !tax.IsZero() {
		txn.TaxAmount = &tax
	}
	if coupon != nil {
		txn.CouponID = coupon.CouponID
	}
}

func (uc *ChargeUseCase) postAuthTokenStorage(ctx context.Context, result *port.GatewayAuthResult, req PurchaseRequest, savedCardID, targetChannel string) {
	if result.RecurringToken == "" {
		return
	}

	if savedCardID != "" {
		// 复购路径：回存 ChannelToken
		if err := uc.cardCommand.StoreChannelToken(ctx, savedCardID, result.Channel, result.RecurringToken, result.ProviderRef); err != nil {
			log.Printf("[UseCase] StoreChannelToken failed (card=%s, channel=%s): %v", savedCardID, result.Channel, err)
		}
		return
	}

	if req.SaveCard {
		// 首次路径 + SaveCard：绑卡
		cardID, err := uc.cardCommand.BindCardFromToken(ctx, port.BindFromTokenCommand{
			CardToken:  req.Token.TokenID,
			Channel:    result.Channel,
			Token:      result.RecurringToken,
			ShopperRef: result.ProviderRef,
		})
		if err != nil {
			log.Printf("[UseCase] BindCardFromToken failed: %v", err)
		} else {
			log.Printf("[UseCase] Card bound: cardID=%s", cardID)
		}
	}
}

func (uc *ChargeUseCase) rollbackCoupon(ctx context.Context, couponCode string) {
	if couponCode == "" || uc.couponApplier == nil {
		return
	}
	if err := uc.couponApplier.Rollback(ctx, couponCode); err != nil {
		log.Printf("[UseCase] coupon rollback failed: %v", err)
	}
}

func (uc *ChargeUseCase) publishEvents(txn *model.PaymentTransaction) {
	for _, evt := range txn.ClearEvents() {
		log.Printf("[DomainEvent] %s: %+v", evt.EventName(), evt)
	}
}
