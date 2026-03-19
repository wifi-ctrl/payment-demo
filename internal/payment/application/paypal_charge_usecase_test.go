package application_test

import (
	"context"
	"errors"
	"testing"

	"payment-demo/internal/payment/adapter/persistence"
	"payment-demo/internal/payment/application"
	"payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// ═══════════════════════════════════════════════════════════════════
// PayPal 专用 Stubs
// ═══════════════════════════════════════════════════════════════════

// stubPayPalGateway 可控制 Authorize 返回值
type stubPayPalGateway struct {
	authorizeResult *port.PayPalAuthResult
	authorizeErr    error
	authorizeCalled bool
	authorizedWith  *model.PayPalToken
}

func (g *stubPayPalGateway) Authorize(_ context.Context, token model.PayPalToken, _ model.Money) (*port.PayPalAuthResult, error) {
	g.authorizeCalled = true
	g.authorizedWith = &token
	return g.authorizeResult, g.authorizeErr
}

func (g *stubPayPalGateway) Capture(_ context.Context, _ string, _ model.Money) error {
	return nil
}

func (g *stubPayPalGateway) Refund(_ context.Context, _ string, _ model.Money) error {
	return nil
}

// stubPayPalFullGateway 追踪 Authorize / Capture / Refund 全部调用
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

func (g *stubPayPalFullGateway) Authorize(_ context.Context, _ model.PayPalToken, _ model.Money) (*port.PayPalAuthResult, error) {
	g.authorizeCalled = true
	return g.authorizeResult, g.authorizeErr
}

func (g *stubPayPalFullGateway) Capture(_ context.Context, providerRef string, _ model.Money) error {
	g.captureCalled = true
	g.capturedRef = providerRef
	return g.captureErr
}

func (g *stubPayPalFullGateway) Refund(_ context.Context, providerRef string, _ model.Money) error {
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

func (g *stubCardGatewayFull) Authorize(_ context.Context, _ model.CardToken, _ model.Money) (*port.GatewayAuthResult, error) {
	return g.authorizeResult, g.authorizeErr
}

func (g *stubCardGatewayFull) Capture(_ context.Context, _ string, _ model.Money) error {
	g.captureCalled = true
	return g.captureErr
}

func (g *stubCardGatewayFull) Refund(_ context.Context, _ string, _ model.Money) error {
	g.refundCalled = true
	return g.refundErr
}

// spyPayPalGateway 可观测 Authorize 调用次数（商户路由验证用）
type spyPayPalGateway struct {
	authorizeResult *port.PayPalAuthResult
	authorizeErr    error
	authorizeCount  int
}

func (g *spyPayPalGateway) Authorize(_ context.Context, _ model.PayPalToken, _ model.Money) (*port.PayPalAuthResult, error) {
	g.authorizeCount++
	return g.authorizeResult, g.authorizeErr
}
func (g *spyPayPalGateway) Capture(_ context.Context, _ string, _ model.Money) error { return nil }
func (g *spyPayPalGateway) Refund(_ context.Context, _ string, _ model.Money) error  { return nil }

// stubRepoWithError 可模拟 Save 失败的仓储
type stubRepoWithError struct {
	inner     port.TransactionRepository
	saveErr   error
	saveCalls int
}

func (r *stubRepoWithError) Save(ctx context.Context, txn *model.PaymentTransaction) error {
	r.saveCalls++
	if r.saveErr != nil {
		return r.saveErr
	}
	return r.inner.Save(ctx, txn)
}

func (r *stubRepoWithError) FindByID(ctx context.Context, id model.TransactionID) (*model.PaymentTransaction, error) {
	return r.inner.FindByID(ctx, id)
}

// dualChannelMerchantQuery 支持按渠道返回不同凭据
type dualChannelMerchantQuery struct {
	cardCred   *port.ChannelCredentialView
	paypalCred *port.ChannelCredentialView
	err        error
}

func (q *dualChannelMerchantQuery) FindActiveCredential(_ context.Context, _ string, channel model.PaymentMethod) (*port.ChannelCredentialView, error) {
	if q.err != nil {
		return nil, q.err
	}
	if channel == model.PaymentMethodPayPal {
		return q.paypalCred, nil
	}
	return q.cardCred, nil
}

// ═══════════════════════════════════════════════════════════════════
// UseCase 组装辅助
// ═══════════════════════════════════════════════════════════════════

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
	return application.NewChargeUseCase(merchantQ, factory, repo, catalog, cardQuery, nil, nil)
}

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
	return application.NewChargeUseCase(merchantQ, factory, repo, catalog, cardQuery, nil, nil)
}

// ═══════════════════════════════════════════════════════════════════
// PayPal Purchase 测试
// ═══════════════════════════════════════════════════════════════════

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
		Token:      model.PayPalToken{OrderID: "5O190127TN364715T", PayerID: "FSMVU44LF3YUS"},
	})
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}

	if !paypalGw.authorizeCalled {
		t.Error("PayPalGateway.Authorize must be called")
	}
	if paypalGw.authorizedWith.OrderID != "5O190127TN364715T" {
		t.Errorf("want OrderID=5O190127TN364715T, got %s", paypalGw.authorizedWith.OrderID)
	}
	if paypalGw.authorizedWith.PayerID != "FSMVU44LF3YUS" {
		t.Errorf("want PayerID=FSMVU44LF3YUS, got %s", paypalGw.authorizedWith.PayerID)
	}
	if txn.Status != model.StatusAuthorized {
		t.Errorf("want AUTHORIZED, got %s", txn.Status)
	}
	if txn.Method != model.PaymentMethodPayPal {
		t.Errorf("want Method=PAYPAL, got %s", txn.Method)
	}
	if txn.ProviderRef != "CAPTURE-12345" {
		t.Errorf("want ProviderRef=CAPTURE-12345, got %s", txn.ProviderRef)
	}
	if len(txn.Events) != 0 {
		t.Errorf("events should be cleared, got %d", len(txn.Events))
	}
	if txn.MerchantID != "merchant-1" {
		t.Errorf("MerchantID: want merchant-1, got %s", txn.MerchantID)
	}
}

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
		Token:      model.PayPalToken{OrderID: "5O190127TN364715T", PayerID: "FSMVU44LF3YUS"},
	})

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if txn.Status != model.StatusAuthorized {
		t.Errorf("Status: want AUTHORIZED, got %s", txn.Status)
	}
	if txn.Method != model.PaymentMethodPayPal {
		t.Errorf("Method: want PAYPAL, got %s", txn.Method)
	}
	if txn.ProviderRef != "CAPTURE-001" {
		t.Errorf("ProviderRef: want CAPTURE-001, got %s", txn.ProviderRef)
	}
	if txn.Amount.Amount != 1000 || txn.Amount.Currency != "USD" {
		t.Errorf("Amount: want {1000 USD}, got %+v", txn.Amount)
	}
	if len(txn.Events) != 0 {
		t.Errorf("Events must be cleared after publishEvents, got %d", len(txn.Events))
	}
	saved, err := repo.FindByID(context.Background(), txn.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if saved.Status != model.StatusAuthorized {
		t.Errorf("saved Status: want AUTHORIZED, got %s", saved.Status)
	}
	if !paypalGw.authorizeCalled {
		t.Error("PayPalGateway.Authorize must be called")
	}
}

func TestChargeUseCase_PayPalPurchase_TokenDeclined_ReturnsError(t *testing.T) {
	paypalGw := &stubPayPalFullGateway{
		authorizeErr: model.ErrPayPalTokenInvalid,
	}
	repo := persistence.NewInMemoryTransactionRepository()
	uc := buildUseCase(&stubCardGatewayFull{}, paypalGw, repo,
		&stubCatalog{product: activeProduct()}, &stubCardQuery{})

	txn, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "u1",
		ProductID:  "p1",
		Token:      model.PayPalToken{OrderID: "EC-DECLINE-001", PayerID: "PAYER-001"},
	})

	if !errors.Is(err, model.ErrAuthorizationDeclined) {
		t.Errorf("want ErrAuthorizationDeclined, got %v", err)
	}
	if txn == nil {
		t.Fatal("want failed txn returned, got nil")
	}
	if txn.Status != model.StatusFailed {
		t.Errorf("want Status=FAILED, got %s", txn.Status)
	}
	if txn.FailReason == "" {
		t.Error("FailReason must not be empty after authorization failure")
	}
	saved, findErr := repo.FindByID(context.Background(), txn.ID)
	if findErr != nil {
		t.Fatalf("want failed txn persisted in repo, got error: %v", findErr)
	}
	if saved.Status != model.StatusFailed {
		t.Errorf("saved Status: want FAILED, got %s", saved.Status)
	}
}

func TestChargeUseCase_PayPalPurchase_ProductNotFound_DoesNotCallGateway(t *testing.T) {
	paypalGw := &stubPayPalFullGateway{}
	repo := &stubRepoWithError{inner: persistence.NewInMemoryTransactionRepository()}

	uc := buildUseCase(&stubCardGatewayFull{}, paypalGw, repo,
		&stubCatalog{err: model.ErrProductNotFound}, &stubCardQuery{})

	_, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "user-1",
		ProductID:  "prod-nonexistent",
		Token:      model.PayPalToken{OrderID: "5O190127", PayerID: "PAYER-1"},
	})

	if !errors.Is(err, model.ErrProductNotFound) {
		t.Errorf("want ErrProductNotFound, got %v", err)
	}
	if paypalGw.authorizeCalled {
		t.Error("PayPalGateway.Authorize must NOT be called when product is not found")
	}
	if repo.saveCalls != 0 {
		t.Errorf("repo.Save must NOT be called, got %d calls", repo.saveCalls)
	}
}

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
		Token:      model.PayPalToken{OrderID: "5O190127", PayerID: "PAYER"},
	})

	if !errors.Is(err, model.ErrProductNotActive) {
		t.Errorf("want ErrProductNotActive, got %v", err)
	}
	if paypalGw.authorizeCalled {
		t.Error("PayPalGateway.Authorize must NOT be called")
	}
	if repo.saveCalls != 0 {
		t.Errorf("repo.Save must NOT be called, got %d calls", repo.saveCalls)
	}
}

func TestChargeUseCase_PayPalPurchase_MissingMerchantID_ReturnsError(t *testing.T) {
	paypalGw := &stubPayPalGateway{}
	uc := buildPayPalUseCase(&stubGateway{}, paypalGw, &stubCatalog{product: activeProduct()}, &stubCardQuery{})

	_, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "",
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      model.PayPalToken{OrderID: "5O190127", PayerID: "PAYER"},
	})
	if !errors.Is(err, model.ErrMerchantRequired) {
		t.Errorf("want ErrMerchantRequired, got %v", err)
	}
	if paypalGw.authorizeCalled {
		t.Error("Authorize must NOT be called when MerchantID is missing")
	}
}

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
		Token:      model.PayPalToken{OrderID: "5O190127TN", PayerID: "PAYER-01"},
	})

	if err == nil {
		t.Fatal("want error when repo.Save fails, got nil")
	}
	if !errors.Is(err, saveErr) {
		t.Errorf("want saveErr, got %v", err)
	}
}

// ═══════════════════════════════════════════════════════════════════
// PayPal — 商户路由专项测试
// ═══════════════════════════════════════════════════════════════════

func TestChargeUseCase_PayPalPurchase_MerchantRouting_Success(t *testing.T) {
	const merchantID = "merchant-paypal-001"

	merchantQ := &spyMerchantQuery{cred: merchantPayPalCredView(merchantID)}
	paypalGw := &spyPayPalGateway{
		authorizeResult: &port.PayPalAuthResult{ProviderRef: "PAYPAL-CAPTURE-001", PayerEmail: "buyer@example.com"},
	}
	gwFactory := &spyGatewayFactory{paypalGateway: paypalGw}
	txnRepo := persistence.NewInMemoryTransactionRepository()
	catalog := &stubCatalog{product: activeProduct()}
	cardQ := &stubCardQuery{}

	uc := application.NewChargeUseCase(merchantQ, gwFactory, txnRepo, catalog, cardQ, nil, nil)

	txn, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: merchantID,
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      model.PayPalToken{OrderID: "5O190127TN364715T", PayerID: "FSMVU44LF3YUS"},
	})

	if err != nil {
		t.Fatalf("PayPalPurchase: want nil error, got %v", err)
	}
	if txn == nil {
		t.Fatal("PayPalPurchase: want non-nil transaction")
	}
	if txn.Status != model.StatusAuthorized {
		t.Errorf("Status: want AUTHORIZED, got %s", txn.Status)
	}
	if merchantQ.callCount != 1 {
		t.Errorf("MerchantQuery calls: want 1, got %d", merchantQ.callCount)
	}
	if merchantQ.lastChannel != model.PaymentMethodPayPal {
		t.Errorf("MerchantQuery channel: want PAYPAL, got %s", merchantQ.lastChannel)
	}
	if gwFactory.paypalBuildCount != 1 {
		t.Errorf("BuildPayPalGateway calls: want 1, got %d", gwFactory.paypalBuildCount)
	}
	if gwFactory.lastPayPalCred.CredentialID != "cred-paypal-1" {
		t.Errorf("BuildPayPalGateway cred ID: want cred-paypal-1, got %s", gwFactory.lastPayPalCred.CredentialID)
	}
	if paypalGw.authorizeCount != 1 {
		t.Errorf("PayPal Authorize calls: want 1, got %d", paypalGw.authorizeCount)
	}
	if txn.MerchantID != merchantID {
		t.Errorf("txn.MerchantID: want %s, got %s", merchantID, txn.MerchantID)
	}
}

func TestChargeUseCase_PayPalPurchase_MerchantCredentialNotFound_DoesNotSave(t *testing.T) {
	merchantQ := &spyMerchantQuery{err: port.ErrMerchantCredentialNotFound}
	gwFactory := &spyGatewayFactory{}
	txnRepo := persistence.NewInMemoryTransactionRepository()
	catalog := &stubCatalog{product: activeProduct()}
	cardQ := &stubCardQuery{}

	uc := application.NewChargeUseCase(merchantQ, gwFactory, txnRepo, catalog, cardQ, nil, nil)

	_, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-001",
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      model.PayPalToken{OrderID: "order-1", PayerID: "payer-1"},
	})

	if !errors.Is(err, port.ErrMerchantCredentialNotFound) {
		t.Errorf("want ErrMerchantCredentialNotFound, got %v", err)
	}
	if gwFactory.paypalBuildCount != 0 {
		t.Errorf("BuildPayPalGateway must not be called, got %d calls", gwFactory.paypalBuildCount)
	}
}

// ═══════════════════════════════════════════════════════════════════
// Capture / Refund — PayPal 路由测试
// ═══════════════════════════════════════════════════════════════════

func TestChargeUseCase_Capture_PayPalTransaction_RoutesToPayPalGateway(t *testing.T) {
	paypalGw := &stubPayPalFullGateway{
		authorizeResult: &port.PayPalAuthResult{ProviderRef: "CAPTURE-001"},
	}
	cardGw := &stubCardGatewayFull{}
	repo := persistence.NewInMemoryTransactionRepository()
	uc := buildUseCase(cardGw, paypalGw, repo, &stubCatalog{product: activeProduct()}, &stubCardQuery{})

	txn, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "u1",
		ProductID:  "p1",
		Token:      model.PayPalToken{OrderID: "5O190127TN", PayerID: "PAYER-01"},
	})
	if err != nil {
		t.Fatalf("PayPalPurchase: %v", err)
	}

	captured, err := uc.Capture(context.Background(), txn.ID)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if captured.Status != model.StatusCaptured {
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

func TestChargeUseCase_Capture_CardTransaction_RoutesToCardGateway(t *testing.T) {
	cardGw := &stubCardGatewayFull{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "pi_card_001", AuthCode: "AUTH_001"},
	}
	paypalGw := &stubPayPalFullGateway{}
	repo := persistence.NewInMemoryTransactionRepository()
	uc := buildUseCase(cardGw, paypalGw, repo, &stubCatalog{product: activeProduct()}, &stubCardQuery{})

	txn, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "u1",
		ProductID:  "p1",
		Token:      model.CardToken{TokenID: "tok_visa", Last4: "4242", Brand: "Visa"},
	})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}

	captured, err := uc.Capture(context.Background(), txn.ID)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if captured.Status != model.StatusCaptured {
		t.Errorf("Status: want CAPTURED, got %s", captured.Status)
	}
	if !cardGw.captureCalled {
		t.Error("cardGateway.Capture must be called for Card transaction")
	}
	if paypalGw.captureCalled {
		t.Error("paypalGateway.Capture must NOT be called for Card transaction")
	}
}

func TestChargeUseCase_Capture_PayPalGatewayFails_ReturnsError(t *testing.T) {
	paypalGw := &stubPayPalFullGateway{
		authorizeResult: &port.PayPalAuthResult{ProviderRef: "CAPTURE-001"},
		captureErr:      errors.New("paypal capture service unavailable"),
	}
	repo := persistence.NewInMemoryTransactionRepository()
	uc := buildUseCase(&stubCardGatewayFull{}, paypalGw, repo,
		&stubCatalog{product: activeProduct()}, &stubCardQuery{})

	txn, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "u1",
		ProductID:  "p1",
		Token:      model.PayPalToken{OrderID: "5O190127TN", PayerID: "PAYER-01"},
	})
	if err != nil {
		t.Fatalf("PayPalPurchase: %v", err)
	}

	_, captureErr := uc.Capture(context.Background(), txn.ID)
	if !errors.Is(captureErr, model.ErrCaptureFailure) {
		t.Errorf("want ErrCaptureFailure, got %v", captureErr)
	}

	saved, _ := repo.FindByID(context.Background(), txn.ID)
	if saved.Status != model.StatusAuthorized {
		t.Errorf("Status must remain AUTHORIZED after Capture failure, got %s", saved.Status)
	}
}

func TestChargeUseCase_Refund_PayPalTransaction_RoutesToPayPalGateway(t *testing.T) {
	paypalGw := &stubPayPalFullGateway{
		authorizeResult: &port.PayPalAuthResult{ProviderRef: "CAPTURE-001"},
	}
	cardGw := &stubCardGatewayFull{}
	repo := persistence.NewInMemoryTransactionRepository()
	uc := buildUseCase(cardGw, paypalGw, repo, &stubCatalog{product: activeProduct()}, &stubCardQuery{})

	txn, err := uc.PayPalPurchase(context.Background(), application.PayPalPurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "u1",
		ProductID:  "p1",
		Token:      model.PayPalToken{OrderID: "5O190127TN", PayerID: "PAYER-01"},
	})
	if err != nil {
		t.Fatalf("PayPalPurchase: %v", err)
	}

	_, err = uc.Capture(context.Background(), txn.ID)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	refunded, err := uc.Refund(context.Background(), txn.ID)
	if err != nil {
		t.Fatalf("Refund: %v", err)
	}

	if refunded.Status != model.StatusRefunded {
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

	uc := application.NewChargeUseCase(merchantQ, factory, repo, catalog, cardQuery, nil, nil)
	if uc == nil {
		t.Fatal("want non-nil ChargeUseCase, got nil")
	}
}
