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
// stubPayPalFullGateway 同时追踪 Authorize / Capture / Refund 调用情况
// ─────────────────────────────────────────────────────────────────

type stubPayPalFullGateway struct {
	authorizeResult *port.PayPalAuthResult
	authorizeErr    error
	authorizeCalled bool

	captureErr    error
	captureCalled bool
	capturedRef   string

	refundErr    error
	refundCalled bool
	refundedRef  string
}

func (g *stubPayPalFullGateway) Authorize(_ context.Context, _ paymentModel.PayPalToken, _ paymentModel.Money) (*port.PayPalAuthResult, error) {
	g.authorizeCalled = true
	return g.authorizeResult, g.authorizeErr
}

func (g *stubPayPalFullGateway) Capture(_ context.Context, providerRef string, _ paymentModel.Money) error {
	g.captureCalled = true
	g.capturedRef = providerRef
	return g.captureErr
}

func (g *stubPayPalFullGateway) Refund(_ context.Context, providerRef string, _ paymentModel.Money) error {
	g.refundCalled = true
	g.refundedRef = providerRef
	return g.refundErr
}

// stubCardGatewayFull 追踪 Capture / Refund 调用
type stubCardGatewayFull struct {
	authorizeResult *port.GatewayAuthResult
	authorizeErr    error

	captureCalled bool
	captureErr    error

	refundCalled bool
	refundErr    error
}

func (g *stubCardGatewayFull) Authorize(_ context.Context, _ paymentModel.CardToken, _ paymentModel.Money) (*port.GatewayAuthResult, error) {
	return g.authorizeResult, g.authorizeErr
}

func (g *stubCardGatewayFull) Capture(_ context.Context, _ string, _ paymentModel.Money) error {
	g.captureCalled = true
	return g.captureErr
}

func (g *stubCardGatewayFull) Refund(_ context.Context, _ string, _ paymentModel.Money) error {
	g.refundCalled = true
	return g.refundErr
}

// ─────────────────────────────────────────────────────────────────
// stubRepoWithError — 可模拟 Save 失败的仓储
// ─────────────────────────────────────────────────────────────────

type stubRepoWithError struct {
	inner     port.TransactionRepository
	saveErr   error
	saveCalls int
}

func (r *stubRepoWithError) Save(ctx context.Context, txn *paymentModel.PaymentTransaction) error {
	r.saveCalls++
	if r.saveErr != nil {
		return r.saveErr
	}
	return r.inner.Save(ctx, txn)
}

func (r *stubRepoWithError) FindByID(ctx context.Context, id paymentModel.TransactionID) (*paymentModel.PaymentTransaction, error) {
	return r.inner.FindByID(ctx, id)
}

// ─────────────────────────────────────────────────────────────────
// 辅助：构建含完整 spy 能力的 UseCase（多商户版）
// ─────────────────────────────────────────────────────────────────

func buildUseCase(
	cardGw port.PaymentGateway,
	paypalGw port.PayPalGateway,
	repo port.TransactionRepository,
	catalog port.CatalogQuery,
	cardQuery port.CardQuery,
) *application.ChargeUseCase {
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
// AC-20: PayPalPurchase — 正常授权全流程
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_PayPalPurchase_FullFlow_Succeeds(t *testing.T) {
	paypalGw := &stubPayPalFullGateway{
		authorizeResult: &port.PayPalAuthResult{
			ProviderRef: "CAPTURE-001",
			PayerEmail:  "buyer@example.com",
		},
	}
	repo := persistence.NewInMemoryTransactionRepository()
	uc := buildUseCase(&stubCardGatewayFull{}, paypalGw, repo, &stubCatalog{product: activeProduct()}, &stubCardQuery{})

	txn, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "u1",
		ProductID:  "p1",
		Token:      paymentModel.PayPalToken{OrderID: "5O190127TN364715T", PayerID: "FSMVU44LF3YUS"},
	})

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if txn.Status != paymentModel.StatusAuthorized {
		t.Errorf("Status: want AUTHORIZED, got %s", txn.Status)
	}
	if txn.Method != paymentModel.PaymentMethodPayPal {
		t.Errorf("Method: want PAYPAL, got %s", txn.Method)
	}
	if txn.ProviderRef != "CAPTURE-001" {
		t.Errorf("ProviderRef: want CAPTURE-001, got %s", txn.ProviderRef)
	}
	if txn.Amount.Amount != 1000 || txn.Amount.Currency != "USD" {
		t.Errorf("Amount: want {1000 USD}, got %+v", txn.Amount)
	}
	// publishEvents 应清空事件
	if len(txn.Events) != 0 {
		t.Errorf("Events must be cleared after publishEvents, got %d", len(txn.Events))
	}
	// repo 中可查到已持久化的交易
	saved, err := repo.FindByID(context.Background(), txn.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if saved.Status != paymentModel.StatusAuthorized {
		t.Errorf("saved Status: want AUTHORIZED, got %s", saved.Status)
	}
	// paypalGw.Authorize 被调用 1 次
	if !paypalGw.authorizeCalled {
		t.Error("PayPalGateway.Authorize must be called")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-21: PayPalPurchase — 商品不存在，不调用 Gateway，不 Save
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_PayPalPurchase_ProductNotFound_NoGatewayCall(t *testing.T) {
	paypalGw := &stubPayPalFullGateway{}
	repo := &stubRepoWithError{inner: persistence.NewInMemoryTransactionRepository()}

	uc := buildUseCase(&stubCardGatewayFull{}, paypalGw, repo,
		&stubCatalog{err: paymentModel.ErrProductNotFound}, &stubCardQuery{})

	_, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "u1",
		ProductID:  "nonexistent",
		Token:      paymentModel.PayPalToken{OrderID: "5O190127", PayerID: "PAYER"},
	})

	if !errors.Is(err, paymentModel.ErrProductNotFound) {
		t.Errorf("want ErrProductNotFound, got %v", err)
	}
	if paypalGw.authorizeCalled {
		t.Error("PayPalGateway.Authorize must NOT be called")
	}
	if repo.saveCalls != 0 {
		t.Errorf("repo.Save must NOT be called, got %d calls", repo.saveCalls)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-22: PayPalPurchase — 商品未上架，不调用 Gateway，不 Save
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_PayPalPurchase_ProductNotActive_NoGatewayCall(t *testing.T) {
	inactiveProduct := activeProduct()
	inactiveProduct.IsActive = false

	paypalGw := &stubPayPalFullGateway{}
	repo := &stubRepoWithError{inner: persistence.NewInMemoryTransactionRepository()}

	uc := buildUseCase(&stubCardGatewayFull{}, paypalGw, repo,
		&stubCatalog{product: inactiveProduct}, &stubCardQuery{})

	_, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "u1",
		ProductID:  "p1",
		Token:      paymentModel.PayPalToken{OrderID: "5O190127", PayerID: "PAYER"},
	})

	if !errors.Is(err, paymentModel.ErrProductNotActive) {
		t.Errorf("want ErrProductNotActive, got %v", err)
	}
	if paypalGw.authorizeCalled {
		t.Error("PayPalGateway.Authorize must NOT be called")
	}
	if repo.saveCalls != 0 {
		t.Errorf("repo.Save must NOT be called, got %d calls", repo.saveCalls)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-23: PayPalPurchase — PayPal Token 无效，返回 ErrAuthorizationDeclined，持久化失败状态
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_PayPalPurchase_TokenInvalid_PersistsFailedTxn(t *testing.T) {
	paypalGw := &stubPayPalFullGateway{
		authorizeErr: paymentModel.ErrPayPalTokenInvalid,
	}
	repo := persistence.NewInMemoryTransactionRepository()
	uc := buildUseCase(&stubCardGatewayFull{}, paypalGw, repo,
		&stubCatalog{product: activeProduct()}, &stubCardQuery{})

	txn, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "u1",
		ProductID:  "p1",
		Token:      paymentModel.PayPalToken{OrderID: "EC-DECLINE-001", PayerID: "PAYER"},
	})

	// 返回 ErrAuthorizationDeclined（Gateway 错误统一转换）
	if !errors.Is(err, paymentModel.ErrAuthorizationDeclined) {
		t.Errorf("want ErrAuthorizationDeclined, got %v", err)
	}
	if txn == nil {
		t.Fatal("want failed txn returned, got nil")
	}
	if txn.Status != paymentModel.StatusFailed {
		t.Errorf("Status: want FAILED, got %s", txn.Status)
	}
	if txn.FailReason == "" {
		t.Error("FailReason must not be empty after authorization failure")
	}
	saved, findErr := repo.FindByID(context.Background(), txn.ID)
	if findErr != nil {
		t.Fatalf("want failed txn persisted in repo, got error: %v", findErr)
	}
	if saved.Status != paymentModel.StatusFailed {
		t.Errorf("saved Status: want FAILED, got %s", saved.Status)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-24: Capture — PayPal 交易路由到 paypalGateway.Capture
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_Capture_PayPalTransaction_RoutesToPayPalGateway_Full(t *testing.T) {
	paypalGw := &stubPayPalFullGateway{
		authorizeResult: &port.PayPalAuthResult{ProviderRef: "CAPTURE-001"},
	}
	cardGw := &stubCardGatewayFull{}
	repo := persistence.NewInMemoryTransactionRepository()
	uc := buildUseCase(cardGw, paypalGw, repo, &stubCatalog{product: activeProduct()}, &stubCardQuery{})

	// 先完成 PayPal 授权
	txn, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "u1",
		ProductID:  "p1",
		Token:      paymentModel.PayPalToken{OrderID: "5O190127TN", PayerID: "PAYER-01"},
	})
	if err != nil {
		t.Fatalf("PayPalPurchase: %v", err)
	}

	// 执行 Capture
	captured, err := uc.Capture(context.Background(), txn.ID)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if captured.Status != paymentModel.StatusCaptured {
		t.Errorf("Status: want CAPTURED, got %s", captured.Status)
	}
	if !paypalGw.captureCalled {
		t.Error("paypalGateway.Capture must be called")
	}
	if paypalGw.capturedRef != "CAPTURE-001" {
		t.Errorf("capturedRef: want CAPTURE-001, got %s", paypalGw.capturedRef)
	}
	if cardGw.captureCalled {
		t.Error("cardGateway.Capture must NOT be called for PayPal transaction")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-25: Capture — Card 交易仍路由到原 gateway.Capture（回归验证）
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_Capture_CardTransaction_RoutesToCardGateway_Full(t *testing.T) {
	cardGw := &stubCardGatewayFull{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "pi_card_001", AuthCode: "AUTH_001"},
	}
	paypalGw := &stubPayPalFullGateway{}
	repo := persistence.NewInMemoryTransactionRepository()
	uc := buildUseCase(cardGw, paypalGw, repo, &stubCatalog{product: activeProduct()}, &stubCardQuery{})

	// Card 授权
	txn, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "u1",
		ProductID:  "p1",
		Token:      paymentModel.CardToken{TokenID: "tok_visa", Last4: "4242", Brand: "Visa"},
	})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}

	// Capture
	captured, err := uc.Capture(context.Background(), txn.ID)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if captured.Status != paymentModel.StatusCaptured {
		t.Errorf("Status: want CAPTURED, got %s", captured.Status)
	}
	if !cardGw.captureCalled {
		t.Error("cardGateway.Capture must be called for Card transaction")
	}
	if paypalGw.captureCalled {
		t.Error("paypalGateway.Capture must NOT be called for Card transaction")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-26: Refund — PayPal 交易路由到 paypalGateway.Refund
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_Refund_PayPalTransaction_RoutesToPayPalGateway(t *testing.T) {
	paypalGw := &stubPayPalFullGateway{
		authorizeResult: &port.PayPalAuthResult{ProviderRef: "CAPTURE-001"},
	}
	cardGw := &stubCardGatewayFull{}
	repo := persistence.NewInMemoryTransactionRepository()
	uc := buildUseCase(cardGw, paypalGw, repo, &stubCatalog{product: activeProduct()}, &stubCardQuery{})

	// 授权
	txn, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "u1",
		ProductID:  "p1",
		Token:      paymentModel.PayPalToken{OrderID: "5O190127TN", PayerID: "PAYER-01"},
	})
	if err != nil {
		t.Fatalf("PayPalPurchase: %v", err)
	}

	// 扣款
	_, err = uc.Capture(context.Background(), txn.ID)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	// 退款
	refunded, err := uc.Refund(context.Background(), txn.ID)
	if err != nil {
		t.Fatalf("Refund: %v", err)
	}

	if refunded.Status != paymentModel.StatusRefunded {
		t.Errorf("Status: want REFUNDED, got %s", refunded.Status)
	}
	if !paypalGw.refundCalled {
		t.Error("paypalGateway.Refund must be called")
	}
	if paypalGw.refundedRef != "CAPTURE-001" {
		t.Errorf("refundedRef: want CAPTURE-001, got %s", paypalGw.refundedRef)
	}
	if cardGw.refundCalled {
		t.Error("cardGateway.Refund must NOT be called for PayPal transaction")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-27: Capture — paypalGateway.Capture 失败，返回 ErrCaptureFailure，不持久化 Captured 状态
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_Capture_PayPalGatewayFails_ReturnsError(t *testing.T) {
	paypalGw := &stubPayPalFullGateway{
		authorizeResult: &port.PayPalAuthResult{ProviderRef: "CAPTURE-001"},
		captureErr:      errors.New("paypal capture service unavailable"),
	}
	repo := persistence.NewInMemoryTransactionRepository()
	uc := buildUseCase(&stubCardGatewayFull{}, paypalGw, repo,
		&stubCatalog{product: activeProduct()}, &stubCardQuery{})

	// 先授权
	txn, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "u1",
		ProductID:  "p1",
		Token:      paymentModel.PayPalToken{OrderID: "5O190127TN", PayerID: "PAYER-01"},
	})
	if err != nil {
		t.Fatalf("PayPalPurchase: %v", err)
	}

	// Capture 失败
	_, captureErr := uc.Capture(context.Background(), txn.ID)
	if !errors.Is(captureErr, paymentModel.ErrCaptureFailure) {
		t.Errorf("want ErrCaptureFailure, got %v", captureErr)
	}

	// 交易状态应仍为 AUTHORIZED（网关失败不改变状态）
	saved, _ := repo.FindByID(context.Background(), txn.ID)
	if saved.Status != paymentModel.StatusAuthorized {
		t.Errorf("Status must remain AUTHORIZED after Capture failure, got %s", saved.Status)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-28: NewChargeUseCase — 构造函数签名含 merchantQuery + gatewayFactory 参数
// ─────────────────────────────────────────────────────────────────

func TestNewChargeUseCase_WithPayPalGateway_Compiles(t *testing.T) {
	paypalGw := &stubPayPalFullGateway{}
	cardGw := &stubCardGatewayFull{}
	repo := persistence.NewInMemoryTransactionRepository()
	catalog := &stubCatalog{product: activeProduct()}
	cardQuery := &stubCardQuery{}

	merchantQ := &dualChannelMerchantQuery{
		cardCred:   activeMerchantCred(),
		paypalCred: activePayPalMerchantCred(),
	}
	factory := &stubGatewayFactory{cardGateway: cardGw, paypalGateway: paypalGw}

	uc := application.NewChargeUseCase(merchantQ, factory, repo, catalog, cardQuery)
	if uc == nil {
		t.Fatal("want non-nil ChargeUseCase, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-29: PayPalPurchase — repo.Save 在授权成功后失败，返回错误
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_PayPalPurchase_RepoSaveFails_ReturnsError(t *testing.T) {
	saveErr := errors.New("database unavailable")
	repo := &stubRepoWithError{
		inner:   persistence.NewInMemoryTransactionRepository(),
		saveErr: saveErr,
	}
	paypalGw := &stubPayPalFullGateway{
		authorizeResult: &port.PayPalAuthResult{ProviderRef: "CAPTURE-999"},
	}
	uc := buildUseCase(&stubCardGatewayFull{}, paypalGw, repo,
		&stubCatalog{product: activeProduct()}, &stubCardQuery{})

	_, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "u1",
		ProductID:  "p1",
		Token:      paymentModel.PayPalToken{OrderID: "5O190127TN", PayerID: "PAYER-01"},
	})

	if err == nil {
		t.Fatal("want error when repo.Save fails, got nil")
	}
	if !errors.Is(err, saveErr) {
		t.Errorf("want saveErr, got %v", err)
	}
}
