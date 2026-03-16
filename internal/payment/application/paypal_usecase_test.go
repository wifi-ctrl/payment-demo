package application_test

import (
	"context"
	"errors"
	"testing"

	"payment-demo/internal/payment/adapter/persistence"
	"payment-demo/internal/payment/application"
	paymentModel "payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// ─────────────────────────────────────────────────────────────────
// Test Doubles（仅用于 PayPal 相关测试）
// ─────────────────────────────────────────────────────────────────

// stubPayPalGateway 可控制 Authorize 返回值，用于单元测试
type stubPayPalGateway struct {
	authorizeResult *port.PayPalAuthResult
	authorizeErr    error
	authorizeCalled bool
	authorizedWith  *paymentModel.PayPalToken
}

func (g *stubPayPalGateway) Authorize(_ context.Context, token paymentModel.PayPalToken, _ paymentModel.Money) (*port.PayPalAuthResult, error) {
	g.authorizeCalled = true
	g.authorizedWith = &token
	return g.authorizeResult, g.authorizeErr
}

func (g *stubPayPalGateway) Capture(_ context.Context, _ string, _ paymentModel.Money) error {
	return nil
}

func (g *stubPayPalGateway) Refund(_ context.Context, _ string, _ paymentModel.Money) error {
	return nil
}

// ─────────────────────────────────────────────────────────────────
// 辅助：组装含 PayPal 网关的 ChargeUseCase（多商户版）
// merchantQuery 自动根据渠道类型返回正确的凭据
// ─────────────────────────────────────────────────────────────────

// dualChannelMerchantQuery 支持按渠道返回不同凭据的 stub
type dualChannelMerchantQuery struct {
	cardCred   *port.ChannelCredentialView
	paypalCred *port.ChannelCredentialView
	err        error
}

func (q *dualChannelMerchantQuery) FindActiveCredential(_ context.Context, _ string, channel paymentModel.PaymentMethod) (*port.ChannelCredentialView, error) {
	if q.err != nil {
		return nil, q.err
	}
	if channel == paymentModel.PaymentMethodPayPal {
		return q.paypalCred, nil
	}
	return q.cardCred, nil
}

func buildPayPalUseCase(
	cardGw port.PaymentGateway,
	paypalGw port.PayPalGateway,
	catalog port.CatalogQuery,
	cardQuery port.CardQuery,
) *application.ChargeUseCase {
	repo := persistence.NewInMemoryTransactionRepository()
	merchantQ := &dualChannelMerchantQuery{
		cardCred:   activeMerchantCred(),
		paypalCred: activePayPalMerchantCred(),
	}
	factory := &stubGatewayFactory{
		cardGateway:   cardGw,
		paypalGateway: paypalGw,
	}
	return application.NewChargeUseCase(merchantQ, factory, repo, catalog, cardQuery)
}

// ─────────────────────────────────────────────────────────────────
// AC-PayPal-01  PayPal 购买成功 → 交易状态 AUTHORIZED，Method=PAYPAL
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_PayPalPurchase_Succeeds(t *testing.T) {
	paypalGw := &stubPayPalGateway{
		authorizeResult: &port.PayPalAuthResult{
			ProviderRef: "CAPTURE-12345",
			PayerEmail:  "buyer@example.com",
		},
	}
	catalog := &stubCatalog{product: activeProduct()}
	cardQuery := &stubCardQuery{}

	uc := buildPayPalUseCase(&stubGateway{}, paypalGw, catalog, cardQuery)
	txn, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      paymentModel.PayPalToken{OrderID: "5O190127TN364715T", PayerID: "FSMVU44LF3YUS"},
	})
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}

	// Authorize 必须被调用
	if !paypalGw.authorizeCalled {
		t.Error("PayPalGateway.Authorize must be called")
	}
	// Token 透传正确
	if paypalGw.authorizedWith.OrderID != "5O190127TN364715T" {
		t.Errorf("want OrderID=5O190127TN364715T, got %s", paypalGw.authorizedWith.OrderID)
	}
	if paypalGw.authorizedWith.PayerID != "FSMVU44LF3YUS" {
		t.Errorf("want PayerID=FSMVU44LF3YUS, got %s", paypalGw.authorizedWith.PayerID)
	}

	// 交易状态 AUTHORIZED
	if txn.Status != paymentModel.StatusAuthorized {
		t.Errorf("want AUTHORIZED, got %s", txn.Status)
	}
	// Method 必须为 PAYPAL
	if txn.Method != paymentModel.PaymentMethodPayPal {
		t.Errorf("want Method=PAYPAL, got %s", txn.Method)
	}
	// ProviderRef 已写入
	if txn.ProviderRef != "CAPTURE-12345" {
		t.Errorf("want ProviderRef=CAPTURE-12345, got %s", txn.ProviderRef)
	}
	// 领域事件已被 publishEvents 清空
	if len(txn.Events) != 0 {
		t.Errorf("events should be cleared, got %d", len(txn.Events))
	}
	// MerchantID 已写入交易
	if txn.MerchantID != "merchant-1" {
		t.Errorf("MerchantID: want merchant-1, got %s", txn.MerchantID)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-PayPal-02  PayPal 授权被拒 → 返回 ErrAuthorizationDeclined，交易状态 FAILED
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_PayPalPurchase_TokenDeclined_ReturnsError(t *testing.T) {
	paypalGw := &stubPayPalGateway{
		authorizeErr: paymentModel.ErrPayPalTokenInvalid,
	}
	catalog := &stubCatalog{product: activeProduct()}
	cardQuery := &stubCardQuery{}

	uc := buildPayPalUseCase(&stubGateway{}, paypalGw, catalog, cardQuery)
	txn, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      paymentModel.PayPalToken{OrderID: "EC-DECLINE-001", PayerID: "PAYER-001"},
	})

	if !errors.Is(err, paymentModel.ErrAuthorizationDeclined) {
		t.Errorf("want ErrAuthorizationDeclined, got %v", err)
	}
	// 失败交易应被持久化，且状态为 FAILED
	if txn == nil {
		t.Fatal("want failed txn returned, got nil")
	}
	if txn.Status != paymentModel.StatusFailed {
		t.Errorf("want Status=FAILED, got %s", txn.Status)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-PayPal-03  商品不存在 → 返回 ErrProductNotFound，不调用 PayPal 网关
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_PayPalPurchase_ProductNotFound_DoesNotCallGateway(t *testing.T) {
	paypalGw := &stubPayPalGateway{}
	catalog := &stubCatalog{err: paymentModel.ErrProductNotFound}
	cardQuery := &stubCardQuery{}

	uc := buildPayPalUseCase(&stubGateway{}, paypalGw, catalog, cardQuery)
	_, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "user-1",
		ProductID:  "prod-nonexistent",
		Token:      paymentModel.PayPalToken{OrderID: "5O190127", PayerID: "PAYER-1"},
	})

	if !errors.Is(err, paymentModel.ErrProductNotFound) {
		t.Errorf("want ErrProductNotFound, got %v", err)
	}
	if paypalGw.authorizeCalled {
		t.Error("PayPalGateway.Authorize must NOT be called when product is not found")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-PayPal-04  Capture 对 PayPal 交易路由到 PayPal Gateway
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_Capture_PayPalTransaction_RoutesToPayPalGateway(t *testing.T) {
	paypalGw := &stubPayPalGateway{
		authorizeResult: &port.PayPalAuthResult{ProviderRef: "CAPTURE-999"},
	}
	catalog := &stubCatalog{product: activeProduct()}
	cardQuery := &stubCardQuery{}
	cardGw := &stubGateway{}

	repo := persistence.NewInMemoryTransactionRepository()
	merchantQ := &dualChannelMerchantQuery{
		cardCred:   activeMerchantCred(),
		paypalCred: activePayPalMerchantCred(),
	}
	factory := &stubGatewayFactory{
		cardGateway:   cardGw,
		paypalGateway: paypalGw,
	}
	uc := application.NewChargeUseCase(merchantQ, factory, repo, catalog, cardQuery)

	txn, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      paymentModel.PayPalToken{OrderID: "5O190127TN", PayerID: "PAYER-01"},
	})
	if err != nil {
		t.Fatalf("PayPalPurchase failed: %v", err)
	}

	// Capture
	captured, err := uc.Capture(context.Background(), txn.ID)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}
	if captured.Status != paymentModel.StatusCaptured {
		t.Errorf("want CAPTURED, got %s", captured.Status)
	}
	// Card Gateway 的 Authorize/Capture 不应被调用
	if cardGw.authorizeCalled {
		t.Error("Card gateway must NOT be used for PayPal transaction")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-PayPal-05  Card 交易 Capture 仍路由到 Card Gateway（回归）
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_Capture_CardTransaction_RoutesToCardGateway(t *testing.T) {
	cardGw := &stubGateway{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "pi_card_001", AuthCode: "AUTH_001"},
	}
	paypalGw := &stubPayPalGateway{}
	catalog := &stubCatalog{product: activeProduct()}
	cardQuery := &stubCardQuery{}

	repo := persistence.NewInMemoryTransactionRepository()
	merchantQ := &dualChannelMerchantQuery{
		cardCred:   activeMerchantCred(),
		paypalCred: activePayPalMerchantCred(),
	}
	factory := &stubGatewayFactory{
		cardGateway:   cardGw,
		paypalGateway: paypalGw,
	}
	uc := application.NewChargeUseCase(merchantQ, factory, repo, catalog, cardQuery)

	txn, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      paymentModel.CardToken{TokenID: "tok_visa", Last4: "4242", Brand: "Visa"},
	})
	if err != nil {
		t.Fatalf("Purchase failed: %v", err)
	}

	captured, err := uc.Capture(context.Background(), txn.ID)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}
	if captured.Status != paymentModel.StatusCaptured {
		t.Errorf("want CAPTURED, got %s", captured.Status)
	}
	// PayPal Gateway 的 Authorize 不应被调用
	if paypalGw.authorizeCalled {
		t.Error("PayPal gateway must NOT be used for Card transaction")
	}
}

// ─────────────────────────────────────────────────────────────────
// 多商户专项：PayPalPurchase 时 merchant_id 为空 → ErrMerchantRequired
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_PayPalPurchase_MissingMerchantID_ReturnsError(t *testing.T) {
	paypalGw := &stubPayPalGateway{}
	uc := buildPayPalUseCase(&stubGateway{}, paypalGw, &stubCatalog{product: activeProduct()}, &stubCardQuery{})

	_, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "", // 缺失
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      paymentModel.PayPalToken{OrderID: "5O190127", PayerID: "PAYER"},
	})
	if !errors.Is(err, paymentModel.ErrMerchantRequired) {
		t.Errorf("want ErrMerchantRequired, got %v", err)
	}
	if paypalGw.authorizeCalled {
		t.Error("Authorize must NOT be called when MerchantID is missing")
	}
}
