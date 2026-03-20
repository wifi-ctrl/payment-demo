package application

import (
	"context"
	"log"
	"strings"

	"payment-demo/internal/acquiring/domain/model"
	"payment-demo/internal/acquiring/domain/port"
)

// ChargeUseCase 收单用例编排层。
//
// Acquiring 负责"按给定金额扣款"：
//   - 商户路由（MerchantRepository → GatewayFactory）
//   - 网关授权 / 扣款 / 退款
//   - Card 解析（ResolveCardForGateway）
//   - Capture 后绑卡（postCaptureCardBinding）
type ChargeUseCase struct {
	merchantRepo   port.MerchantRepository
	gatewayFactory port.GatewayFactory
	repo           port.TransactionRepository
	cardQuery      port.CardQuery
	cardCommand    port.CardCommand
}

func NewChargeUseCase(
	merchantRepo port.MerchantRepository,
	gatewayFactory port.GatewayFactory,
	repo port.TransactionRepository,
	cardQuery port.CardQuery,
	cardCommand port.CardCommand,
) *ChargeUseCase {
	return &ChargeUseCase{
		merchantRepo:   merchantRepo,
		gatewayFactory: gatewayFactory,
		repo:           repo,
		cardQuery:      cardQuery,
		cardCommand:    cardCommand,
	}
}

// ─────────────────────────────────────────────────────────────────
// Card 购买
// ─────────────────────────────────────────────────────────────────

// PurchaseRequest 用例层入参（由 Order 上下文编排后传入）。
type PurchaseRequest struct {
	MerchantID  string
	UserID      string
	OrderID     string
	Amount      model.Money
	Token       model.CardToken
	SavedCardID string
	SaveCard    bool
}

// Purchase 卡支付用例。
//
// 由 Order 上下文调用，Amount 已由 Order 锁定（原价 - 折扣 + 税）。
//
// 复购路径（SavedCardID 非空）：
//  1. 查已保存卡 → 匹配 ChannelToken[targetChannel]
//  2. 有 ChannelToken → 直接用 recurring token 授权
//  3. 无 ChannelToken → PrepareOneTimeToken 解密 PAN → 用一次性 token 授权
//
// 首次路径（Token 非空）：
//  1. ct_* 时经 CardCommand.ResolveCardForGateway 取权威 last4/brand 并准备网关 token
//  2. Capture 成功后 + SaveCard=true → BindCardFromToken
func (uc *ChargeUseCase) Purchase(ctx context.Context, req PurchaseRequest) (*model.PaymentTransaction, error) {
	if req.MerchantID == "" {
		return nil, model.ErrMerchantRequired
	}

	log.Printf("[UseCase] Purchase: merchant=%s, user=%s, order=%s, amount=%d %s",
		req.MerchantID, req.UserID, req.OrderID, req.Amount.Amount, req.Amount.Currency)

	cred, err := uc.findActiveCredential(ctx, req.MerchantID, model.PaymentMethodCard)
	if err != nil {
		return nil, err
	}
	targetChannel := cred.Channel

	gateway, err := uc.gatewayFactory.BuildCardGateway(*cred)
	if err != nil {
		return nil, model.ErrMerchantGatewayBuildFailed
	}

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

		if recurringToken, ok := cardView.ChannelTokens[targetChannel]; ok {
			cardToken = model.CardToken{TokenID: recurringToken, Last4: cardView.Last4, Brand: cardView.Brand}
			log.Printf("[UseCase] Using recurring token for channel=%s", targetChannel)
		} else {
			oneTimeToken, err := uc.cardCommand.PrepareOneTimeToken(ctx, cardView.CardID, req.UserID)
			if err != nil {
				return nil, err
			}
			cardToken = model.CardToken{TokenID: oneTimeToken, Last4: cardView.Last4, Brand: cardView.Brand}
			log.Printf("[UseCase] No recurring token for channel=%s, using one-time token", targetChannel)
		}
	}

	gatewayToken := cardToken
	if req.SavedCardID == "" && strings.HasPrefix(cardToken.TokenID, "ct_") {
		resolved, err := uc.cardCommand.ResolveCardForGateway(ctx, cardToken.TokenID, req.UserID)
		if err != nil {
			return nil, err
		}
		cardToken.Last4 = resolved.Last4
		cardToken.Brand = resolved.Brand
		gatewayToken = model.CardToken{TokenID: resolved.GatewayToken, Last4: resolved.Last4, Brand: resolved.Brand}
	}

	txn := model.NewPaymentTransaction(req.UserID, req.OrderID, req.Amount, cardToken)
	txn.MerchantID = req.MerchantID

	result, err := gateway.Authorize(ctx, gatewayToken, req.Amount)
	if err != nil {
		txn.MarkFailed(err.Error())
		if saveErr := uc.repo.Save(ctx, txn); saveErr != nil {
			log.Printf("[UseCase] failed to save failed txn: %v", saveErr)
		}
		return txn, model.ErrAuthorizationDeclined
	}

	if err := txn.MarkAuthorized(result.ProviderRef, result.AuthCode); err != nil {
		return nil, err
	}
	txn.RecordAuthContext(result.Channel, result.RecurringToken, savedCardID, req.SaveCard)

	if err := uc.repo.Save(ctx, txn); err != nil {
		return nil, err
	}
	uc.publishEvents(txn)
	return txn, nil
}

// ─────────────────────────────────────────────────────────────────
// PayPal 购买
// ─────────────────────────────────────────────────────────────────

// PayPalPurchaseRequest PayPal 购买用例入参（由 Order 上下文编排后传入）。
type PayPalPurchaseRequest struct {
	MerchantID string
	UserID     string
	OrderID    string
	Amount     model.Money
	Token      model.PayPalToken
}

// PayPalPurchase PayPal 购买用例。
func (uc *ChargeUseCase) PayPalPurchase(ctx context.Context, req PayPalPurchaseRequest) (*model.PaymentTransaction, error) {
	if req.MerchantID == "" {
		return nil, model.ErrMerchantRequired
	}

	log.Printf("[UseCase] PayPalPurchase: merchant=%s, user=%s, order=%s, amount=%d %s",
		req.MerchantID, req.UserID, req.OrderID, req.Amount.Amount, req.Amount.Currency)

	cred, err := uc.findActiveCredential(ctx, req.MerchantID, model.PaymentMethodPayPal)
	if err != nil {
		return nil, err
	}

	paypalGateway, err := uc.gatewayFactory.BuildPayPalGateway(*cred)
	if err != nil {
		return nil, model.ErrMerchantGatewayBuildFailed
	}

	txn := model.NewPayPalTransaction(req.UserID, req.OrderID, req.Amount, req.Token)
	txn.MerchantID = req.MerchantID

	result, err := paypalGateway.Authorize(ctx, req.Token, req.Amount)
	if err != nil {
		txn.MarkFailed(err.Error())
		if saveErr := uc.repo.Save(ctx, txn); saveErr != nil {
			log.Printf("[UseCase] failed to save failed PayPal txn: %v", saveErr)
		}
		return txn, model.ErrAuthorizationDeclined
	}

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
// Capture / Refund
// ─────────────────────────────────────────────────────────────────

// Capture 扣款用例。
//
// Capture 成功后是绑卡/存 ChannelToken 的唯一触发点（除异步 webhook 外）。
func (uc *ChargeUseCase) Capture(ctx context.Context, userID string, txnID model.TransactionID) (*model.PaymentTransaction, error) {
	txn, err := uc.repo.FindByID(ctx, txnID)
	if err != nil {
		return nil, err
	}
	if txn.UserID != userID {
		return nil, model.ErrTransactionNotFound
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

	if bindErr := uc.postCaptureCardBinding(ctx, txn); bindErr != nil {
		log.Printf("[UseCase] CRITICAL: capture succeeded but post-capture card binding failed: %v", bindErr)
	}
	uc.publishEvents(txn)
	return txn, nil
}

// Refund 退款用例。
func (uc *ChargeUseCase) Refund(ctx context.Context, userID string, txnID model.TransactionID) (*model.PaymentTransaction, error) {
	txn, err := uc.repo.FindByID(ctx, txnID)
	if err != nil {
		return nil, err
	}
	if txn.UserID != userID {
		return nil, model.ErrTransactionNotFound
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
	cred, err := uc.findActiveCredential(ctx, txn.MerchantID, txn.Method)
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

func (uc *ChargeUseCase) GetTransaction(ctx context.Context, userID string, txnID model.TransactionID) (*model.PaymentTransaction, error) {
	txn, err := uc.repo.FindByID(ctx, txnID)
	if err != nil {
		return nil, err
	}
	if txn.UserID != userID {
		return nil, model.ErrTransactionNotFound
	}
	return txn, nil
}

// ─────────────────────────────────────────────────────────────────
// Webhook
// ─────────────────────────────────────────────────────────────────

type RecurringTokenCallbackRequest struct {
	ProviderRef    string
	Channel        string
	RecurringToken string
}

func (uc *ChargeUseCase) HandleRecurringTokenCallback(ctx context.Context, req RecurringTokenCallbackRequest) error {
	txn, err := uc.repo.FindByProviderRef(ctx, req.ProviderRef)
	if err != nil {
		return err
	}
	txn.RecordRecurringToken(req.Channel, req.RecurringToken)
	if err := uc.repo.Save(ctx, txn); err != nil {
		return err
	}
	if txn.Status == model.StatusCaptured {
		if bindErr := uc.postCaptureCardBinding(ctx, txn); bindErr != nil {
			log.Printf("[UseCase] CRITICAL: recurring token callback after capture but binding failed: %v", bindErr)
		}
	}
	log.Printf("[UseCase] RecurringToken callback processed (txn=%s, channel=%s, captured=%v)",
		txn.ID, req.Channel, txn.Status == model.StatusCaptured)
	return nil
}

// ─────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────

func (uc *ChargeUseCase) postCaptureCardBinding(ctx context.Context, txn *model.PaymentTransaction) error {
	if txn.Method != model.PaymentMethodCard || txn.RecurringToken == "" {
		return nil
	}
	if txn.SavedCardID != "" {
		if err := uc.cardCommand.StoreChannelToken(ctx, txn.SavedCardID, txn.Channel, txn.RecurringToken, txn.ProviderRef); err != nil {
			log.Printf("[UseCase] postCapture StoreChannelToken failed (card=%s, channel=%s): %v",
				txn.SavedCardID, txn.Channel, err)
			return err
		}
		log.Printf("[UseCase] postCapture ChannelToken stored (card=%s, channel=%s)", txn.SavedCardID, txn.Channel)
		return nil
	}
	if txn.SaveCard {
		cardID, err := uc.cardCommand.BindCardFromToken(ctx, port.BindFromTokenCommand{
			CardToken:  txn.CardToken.TokenID,
			Channel:    txn.Channel,
			Token:      txn.RecurringToken,
			ShopperRef: txn.ProviderRef,
		})
		if err != nil {
			log.Printf("[UseCase] postCapture BindCardFromToken failed: %v", err)
			return err
		}
		log.Printf("[UseCase] postCapture card bound: cardID=%s", cardID)
	}
	return nil
}

func (uc *ChargeUseCase) findActiveCredential(ctx context.Context, merchantID string, method model.PaymentMethod) (*port.ChannelCredentialView, error) {
	m, err := uc.merchantRepo.FindByID(ctx, model.MerchantID(merchantID))
	if err != nil {
		return nil, port.ErrMerchantCredentialNotFound
	}
	if m.Status != model.MerchantStatusActive {
		return nil, port.ErrMerchantCredentialNotFound
	}
	cred, err := m.ActiveCredential(method)
	if err != nil {
		return nil, port.ErrMerchantCredentialNotFound
	}
	return &port.ChannelCredentialView{
		CredentialID: string(cred.ID),
		MerchantID:   merchantID,
		Channel:      string(cred.Channel),
		Secrets:      cred.Secrets,
	}, nil
}

func (uc *ChargeUseCase) publishEvents(txn *model.PaymentTransaction) {
	for _, evt := range txn.ClearEvents() {
		log.Printf("[DomainEvent] %s: %s", evt.EventName(), evt)
	}
}
