package application_test

import (
	"context"
	"errors"
	"testing"

	couponModel "payment-demo/internal/coupon/domain/model"
	"payment-demo/internal/payment/adapter/persistence"
	"payment-demo/internal/payment/application"
	"payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// ═══════════════════════════════════════════════════════════════════
// Shared Stubs — 所有 application_test 文件共用
// ═══════════════════════════════════════════════════════════════════

// --- Card Gateway ---

// stubGateway 可控制 Authorize 返回
type stubGateway struct {
	authorizeResult *port.GatewayAuthResult
	authorizeErr    error
	authorizedWith  *model.CardToken
	authorizeCalled bool
}

func (g *stubGateway) Authorize(_ context.Context, token model.CardToken, _ model.Money) (*port.GatewayAuthResult, error) {
	g.authorizeCalled = true
	g.authorizedWith = &token
	return g.authorizeResult, g.authorizeErr
}

func (g *stubGateway) Capture(_ context.Context, _ string, _ model.Money) error { return nil }
func (g *stubGateway) Refund(_ context.Context, _ string, _ model.Money) error  { return nil }

// stubNoOpPayPalGateway 占位 PayPal Gateway，Card 测试不会调用它
type stubNoOpPayPalGateway struct{}

func (g *stubNoOpPayPalGateway) Authorize(_ context.Context, _ model.PayPalToken, _ model.Money) (*port.PayPalAuthResult, error) {
	return nil, nil
}
func (g *stubNoOpPayPalGateway) Capture(_ context.Context, _ string, _ model.Money) error {
	return nil
}
func (g *stubNoOpPayPalGateway) Refund(_ context.Context, _ string, _ model.Money) error {
	return nil
}

// --- Catalog ---

type stubCatalog struct {
	product *port.ProductView
	err     error
}

func (c *stubCatalog) FindProduct(_ context.Context, _ string) (*port.ProductView, error) {
	return c.product, c.err
}

func activeProduct() *port.ProductView {
	return &port.ProductView{
		ID:       "prod-1",
		Name:     "Widget",
		Amount:   1000,
		Currency: "USD",
		IsActive: true,
	}
}

// --- CardQuery ---

type stubCardQuery struct {
	view *port.SavedCardView
	err  error
}

func (q *stubCardQuery) FindActiveCard(_ context.Context, _ string) (*port.SavedCardView, error) {
	return q.view, q.err
}

type spyCardQuery struct {
	called bool
}

func (q *spyCardQuery) FindActiveCard(_ context.Context, _ string) (*port.SavedCardView, error) {
	q.called = true
	return nil, nil
}

// --- MerchantQuery ---

type stubMerchantQuery struct {
	cred *port.ChannelCredentialView
	err  error
}

func (m *stubMerchantQuery) FindActiveCredential(_ context.Context, _ string, _ model.PaymentMethod) (*port.ChannelCredentialView, error) {
	return m.cred, m.err
}

func activeMerchantCred() *port.ChannelCredentialView {
	return &port.ChannelCredentialView{
		CredentialID: "cred-1",
		MerchantID:   "merchant-1",
		Channel:      "CARD",
		Secrets:      map[string]string{"api_key": "sk_test_xxx"},
	}
}

func activePayPalMerchantCred() *port.ChannelCredentialView {
	return &port.ChannelCredentialView{
		CredentialID: "cred-2",
		MerchantID:   "merchant-1",
		Channel:      "PAYPAL",
		Secrets:      map[string]string{"client_id": "cl_xxx", "client_secret": "sec_xxx"},
	}
}

// --- GatewayFactory ---

type stubGatewayFactory struct {
	cardGateway   port.PaymentGateway
	paypalGateway port.PayPalGateway
	buildCardErr  error
	buildPPErr    error
}

func (f *stubGatewayFactory) BuildCardGateway(_ port.ChannelCredentialView) (port.PaymentGateway, error) {
	return f.cardGateway, f.buildCardErr
}

func (f *stubGatewayFactory) BuildPayPalGateway(_ port.ChannelCredentialView) (port.PayPalGateway, error) {
	return f.paypalGateway, f.buildPPErr
}

// --- UseCase 组装辅助 ---

func buildChargeUseCase(
	cardGw port.PaymentGateway,
	catalog port.CatalogQuery,
	cardQuery port.CardQuery,
) *application.ChargeUseCase {
	repo := persistence.NewInMemoryTransactionRepository()
	merchantQ := &stubMerchantQuery{cred: activeMerchantCred()}
	factory := &stubGatewayFactory{
		cardGateway:   cardGw,
		paypalGateway: &stubNoOpPayPalGateway{},
	}
	return application.NewChargeUseCase(merchantQ, factory, repo, catalog, cardQuery, nil, nil)
}

// --- Spy stubs（商户路由验证用）---

// spyMerchantQuery 可观测调用次数和最后一次 channel 参数
type spyMerchantQuery struct {
	cred        *port.ChannelCredentialView
	err         error
	callCount   int
	lastChannel model.PaymentMethod
}

func (s *spyMerchantQuery) FindActiveCredential(
	_ context.Context,
	_ string,
	channel model.PaymentMethod,
) (*port.ChannelCredentialView, error) {
	s.callCount++
	s.lastChannel = channel
	return s.cred, s.err
}

// spyGatewayFactory 可观测 Build 调用次数和最后传入凭据
type spyGatewayFactory struct {
	cardGateway      port.PaymentGateway
	paypalGateway    port.PayPalGateway
	cardBuildErr     error
	paypalBuildErr   error
	cardBuildCount   int
	paypalBuildCount int
	lastCardCred     port.ChannelCredentialView
	lastPayPalCred   port.ChannelCredentialView
}

func (f *spyGatewayFactory) BuildCardGateway(cred port.ChannelCredentialView) (port.PaymentGateway, error) {
	f.cardBuildCount++
	f.lastCardCred = cred
	if f.cardBuildErr != nil {
		return nil, f.cardBuildErr
	}
	return f.cardGateway, nil
}

func (f *spyGatewayFactory) BuildPayPalGateway(cred port.ChannelCredentialView) (port.PayPalGateway, error) {
	f.paypalBuildCount++
	f.lastPayPalCred = cred
	if f.paypalBuildErr != nil {
		return nil, f.paypalBuildErr
	}
	return f.paypalGateway, nil
}

// spyCardGateway 可观测 Authorize 调用次数
type spyCardGateway struct {
	authorizeResult *port.GatewayAuthResult
	authorizeErr    error
	authorizeCount  int
}

func (g *spyCardGateway) Authorize(_ context.Context, _ model.CardToken, _ model.Money) (*port.GatewayAuthResult, error) {
	g.authorizeCount++
	return g.authorizeResult, g.authorizeErr
}
func (g *spyCardGateway) Capture(_ context.Context, _ string, _ model.Money) error { return nil }
func (g *spyCardGateway) Refund(_ context.Context, _ string, _ model.Money) error  { return nil }

func merchantCardCredView(merchantID string) *port.ChannelCredentialView {
	return &port.ChannelCredentialView{
		CredentialID: "cred-card-1",
		MerchantID:   merchantID,
		Channel:      "CARD",
		Secrets:      map[string]string{"api_key": "sk_live_xxx"},
	}
}

func merchantPayPalCredView(merchantID string) *port.ChannelCredentialView {
	return &port.ChannelCredentialView{
		CredentialID: "cred-paypal-1",
		MerchantID:   merchantID,
		Channel:      "PAYPAL",
		Secrets:      map[string]string{"client_id": "id", "client_secret": "sec"},
	}
}

// --- Pricing stubs（优惠券 + 税率）---

type stubCouponApplier struct {
	applied       *port.AppliedCoupon
	applyErr      error
	rollbackCalls int
}

func (s *stubCouponApplier) Apply(_ context.Context, _ string, _ string) (*port.AppliedCoupon, error) {
	return s.applied, s.applyErr
}

func (s *stubCouponApplier) Rollback(_ context.Context, _ string) error {
	s.rollbackCalls++
	return nil
}

type stubTaxQuery struct {
	bp  int64
	err error
}

func (s *stubTaxQuery) FindTaxRate(_ context.Context, _ string, _ string) (int64, error) {
	return s.bp, s.err
}

// spyTxnRepo 可追踪 Save 调用次数
type spyTxnRepo struct {
	saveCalls int
	savedTxn  *model.PaymentTransaction
	findErr   error
}

func (s *spyTxnRepo) Save(_ context.Context, txn *model.PaymentTransaction) error {
	s.saveCalls++
	s.savedTxn = txn
	return nil
}

func (s *spyTxnRepo) FindByID(_ context.Context, _ model.TransactionID) (*model.PaymentTransaction, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	return s.savedTxn, nil
}

// ═══════════════════════════════════════════════════════════════════
// Card Purchase — SavedCard 测试
// ═══════════════════════════════════════════════════════════════════

func TestChargeUseCase_Purchase_WithSavedCard_Succeeds(t *testing.T) {
	gw := &stubGateway{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "pi_001", AuthCode: "AUTH_001"},
	}
	catalog := &stubCatalog{product: activeProduct()}
	cardQuery := &stubCardQuery{
		view: &port.SavedCardView{
			CardID:   "card-1",
			UserID:   "user-1",
			Token:    "tok_saved",
			Last4:    "4242",
			Brand:    "Visa",
			IsActive: true,
		},
	}

	uc := buildChargeUseCase(gw, catalog, cardQuery)
	txn, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID:  "merchant-1",
		UserID:      "user-1",
		ProductID:   "prod-1",
		SavedCardID: "card-1",
	})
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}

	if !gw.authorizeCalled {
		t.Error("Authorize must be called")
	}
	if gw.authorizedWith.TokenID != "tok_saved" {
		t.Errorf("Authorize TokenID: want tok_saved, got %s", gw.authorizedWith.TokenID)
	}
	if gw.authorizedWith.Last4 != "4242" {
		t.Errorf("Authorize Last4: want 4242, got %s", gw.authorizedWith.Last4)
	}
	if gw.authorizedWith.Brand != "Visa" {
		t.Errorf("Authorize Brand: want Visa, got %s", gw.authorizedWith.Brand)
	}
	if txn.Status != model.StatusAuthorized {
		t.Errorf("want AUTHORIZED, got %s", txn.Status)
	}
	if len(txn.Events) != 0 {
		t.Errorf("events should be cleared, got %d", len(txn.Events))
	}
}

func TestChargeUseCase_Purchase_WithSuspendedSavedCard_ReturnsError(t *testing.T) {
	gw := &stubGateway{}
	catalog := &stubCatalog{product: activeProduct()}
	cardQuery := &stubCardQuery{
		view: &port.SavedCardView{
			CardID:   "card-sus",
			UserID:   "user-1",
			Token:    "tok_sus",
			IsActive: false,
		},
	}

	uc := buildChargeUseCase(gw, catalog, cardQuery)
	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID:  "merchant-1",
		UserID:      "user-1",
		ProductID:   "prod-1",
		SavedCardID: "card-sus",
	})
	if err == nil {
		t.Fatal("want error for suspended card, got nil")
	}
	if !errors.Is(err, model.ErrCardNotUsable) {
		t.Errorf("want ErrCardNotUsable, got %v", err)
	}
	if gw.authorizeCalled {
		t.Error("Authorize must NOT be called for inactive card")
	}
}

func TestChargeUseCase_Purchase_CardQueryFails_ReturnsError(t *testing.T) {
	gw := &stubGateway{}
	catalog := &stubCatalog{product: activeProduct()}
	cardQuery := &stubCardQuery{
		err: model.ErrCardNotFound,
	}

	uc := buildChargeUseCase(gw, catalog, cardQuery)
	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID:  "merchant-1",
		UserID:      "user-1",
		ProductID:   "prod-1",
		SavedCardID: "card-999",
	})
	if err == nil {
		t.Fatal("want error when CardQuery fails, got nil")
	}
	if gw.authorizeCalled {
		t.Error("Authorize must NOT be called when card lookup fails")
	}
}

func TestChargeUseCase_Purchase_CardBelongsToOtherUser_ReturnsError(t *testing.T) {
	gw := &stubGateway{}
	catalog := &stubCatalog{product: activeProduct()}
	cardQuery := &stubCardQuery{
		view: &port.SavedCardView{
			CardID:   "card-1",
			UserID:   "other-user",
			Token:    "tok_abc",
			IsActive: true,
		},
	}

	uc := buildChargeUseCase(gw, catalog, cardQuery)
	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID:  "merchant-1",
		UserID:      "user-1",
		ProductID:   "prod-1",
		SavedCardID: "card-1",
	})
	if err == nil {
		t.Fatal("want error when card belongs to another user, got nil")
	}
	if gw.authorizeCalled {
		t.Error("Authorize must NOT be called when card ownership check fails")
	}
}

func TestChargeUseCase_Purchase_WithOneTimeToken_DoesNotCallCardQuery(t *testing.T) {
	gw := &stubGateway{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "pi_002", AuthCode: "AUTH_002"},
	}
	catalog := &stubCatalog{product: activeProduct()}

	spy := &spyCardQuery{}
	uc := buildChargeUseCase(gw, catalog, spy)
	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      model.CardToken{TokenID: "tok_onetime", Last4: "1111", Brand: "Mastercard"},
	})
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if spy.called {
		t.Error("CardQuery.FindActiveCard must NOT be called when SavedCardID is empty")
	}
}

func TestChargeUseCase_Purchase_MissingMerchantID_ReturnsError(t *testing.T) {
	gw := &stubGateway{}
	catalog := &stubCatalog{product: activeProduct()}
	uc := buildChargeUseCase(gw, catalog, &stubCardQuery{})

	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "",
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      model.CardToken{TokenID: "tok_xxx"},
	})
	if !errors.Is(err, model.ErrMerchantRequired) {
		t.Errorf("want ErrMerchantRequired, got %v", err)
	}
	if gw.authorizeCalled {
		t.Error("Authorize must NOT be called when MerchantID is missing")
	}
}

func TestChargeUseCase_Purchase_MerchantCredentialNotFound_ReturnsError(t *testing.T) {
	gw := &stubGateway{}
	repo := persistence.NewInMemoryTransactionRepository()
	merchantQ := &stubMerchantQuery{err: port.ErrMerchantCredentialNotFound}
	factory := &stubGatewayFactory{cardGateway: gw, paypalGateway: &stubNoOpPayPalGateway{}}
	uc := application.NewChargeUseCase(merchantQ, factory, repo, &stubCatalog{product: activeProduct()}, &stubCardQuery{}, nil, nil)

	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "merchant-unknown",
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      model.CardToken{TokenID: "tok_xxx"},
	})
	if !errors.Is(err, port.ErrMerchantCredentialNotFound) {
		t.Errorf("want ErrMerchantCredentialNotFound, got %v", err)
	}
	if gw.authorizeCalled {
		t.Error("Authorize must NOT be called when credential is not found")
	}
}

// ═══════════════════════════════════════════════════════════════════
// Card Purchase — 商户路由专项测试
// ═══════════════════════════════════════════════════════════════════

func TestChargeUseCase_Purchase_MerchantRouting_Success(t *testing.T) {
	const merchantID = "merchant-001"

	merchantQ := &spyMerchantQuery{cred: merchantCardCredView(merchantID)}
	cardGw := &spyCardGateway{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "ch_test_001", AuthCode: "AUTH-001"},
	}
	gwFactory := &spyGatewayFactory{cardGateway: cardGw}
	txnRepo := persistence.NewInMemoryTransactionRepository()
	catalog := &stubCatalog{product: activeProduct()}
	cardQ := &stubCardQuery{}

	uc := application.NewChargeUseCase(merchantQ, gwFactory, txnRepo, catalog, cardQ, nil, nil)

	txn, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: merchantID,
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      model.CardToken{TokenID: "tok_visa", Last4: "4242", Brand: "Visa"},
	})

	if err != nil {
		t.Fatalf("Purchase: want nil error, got %v", err)
	}
	if txn == nil {
		t.Fatal("Purchase: want non-nil transaction")
	}
	if txn.Status != model.StatusAuthorized {
		t.Errorf("Status: want AUTHORIZED, got %s", txn.Status)
	}
	if merchantQ.callCount != 1 {
		t.Errorf("MerchantQuery calls: want 1, got %d", merchantQ.callCount)
	}
	if merchantQ.lastChannel != model.PaymentMethodCard {
		t.Errorf("MerchantQuery channel: want CARD, got %s", merchantQ.lastChannel)
	}
	if gwFactory.cardBuildCount != 1 {
		t.Errorf("BuildCardGateway calls: want 1, got %d", gwFactory.cardBuildCount)
	}
	if gwFactory.lastCardCred.CredentialID != "cred-card-1" {
		t.Errorf("BuildCardGateway cred ID: want cred-card-1, got %s", gwFactory.lastCardCred.CredentialID)
	}
	if cardGw.authorizeCount != 1 {
		t.Errorf("Authorize calls: want 1, got %d", cardGw.authorizeCount)
	}
	if len(txn.Events) != 0 {
		t.Errorf("Events must be cleared, got %d", len(txn.Events))
	}
	if txn.MerchantID != merchantID {
		t.Errorf("txn.MerchantID: want %s, got %s", merchantID, txn.MerchantID)
	}
}

func TestChargeUseCase_Purchase_MerchantCredentialNotFound_DoesNotSave(t *testing.T) {
	merchantQ := &spyMerchantQuery{err: port.ErrMerchantCredentialNotFound}
	gwFactory := &spyGatewayFactory{}
	txnRepo := persistence.NewInMemoryTransactionRepository()
	catalog := &stubCatalog{product: activeProduct()}
	cardQ := &stubCardQuery{}

	uc := application.NewChargeUseCase(merchantQ, gwFactory, txnRepo, catalog, cardQ, nil, nil)

	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "merchant-001",
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      model.CardToken{TokenID: "tok_visa"},
	})

	if !errors.Is(err, port.ErrMerchantCredentialNotFound) {
		t.Errorf("want ErrMerchantCredentialNotFound, got %v", err)
	}
	if gwFactory.cardBuildCount != 0 {
		t.Errorf("BuildCardGateway must not be called when MerchantQuery fails, got %d calls", gwFactory.cardBuildCount)
	}
}

func TestChargeUseCase_Purchase_GatewayBuildFails_ReturnsError(t *testing.T) {
	merchantQ := &spyMerchantQuery{cred: merchantCardCredView("merchant-1")}
	gwFactory := &spyGatewayFactory{
		cardBuildErr: errors.New("missing api_key"),
	}
	txnRepo := persistence.NewInMemoryTransactionRepository()
	catalog := &stubCatalog{product: activeProduct()}
	cardQ := &stubCardQuery{}

	uc := application.NewChargeUseCase(merchantQ, gwFactory, txnRepo, catalog, cardQ, nil, nil)

	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      model.CardToken{TokenID: "tok_visa"},
	})

	if !errors.Is(err, model.ErrMerchantGatewayBuildFailed) {
		t.Errorf("want ErrMerchantGatewayBuildFailed, got %v", err)
	}
}

// ═══════════════════════════════════════════════════════════════════
// Card Purchase — 定价集成测试（优惠券 + 税率）
// ═══════════════════════════════════════════════════════════════════

func TestPurchase_WithCouponCode_UsesFinalAmount(t *testing.T) {
	txnRepo := &spyTxnRepo{}
	catalogQ := &stubCatalog{
		product: &port.ProductView{
			ID: "p1", Name: "Product 1", Amount: 10000, Currency: "USD", IsActive: true,
		},
	}
	gw := &stubGateway{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "ref-1", AuthCode: "auth-1"},
	}
	couponApplier := &stubCouponApplier{
		applied: &port.AppliedCoupon{CouponID: "cpn-1", DiscountType: "PERCENTAGE", DiscountValue: 1000},
	}
	taxQ := &stubTaxQuery{bp: 1000}

	merchantQ := &stubMerchantQuery{cred: activeMerchantCred()}
	factory := &stubGatewayFactory{cardGateway: gw, paypalGateway: &stubNoOpPayPalGateway{}}
	uc := application.NewChargeUseCase(merchantQ, factory, txnRepo, catalogQ, &stubCardQuery{}, couponApplier, taxQ)

	txn, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "m1", UserID: "u1", ProductID: "p1",
		Token:      model.CardToken{TokenID: "tok-1", Last4: "4242", Brand: "VISA"},
		CouponCode: "SAVE10",
	})

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	// 原价 10000, 折扣 10%=1000, 折后 9000, 税 10%=900, 最终 9900
	if txn.Amount.Amount != 9900 {
		t.Errorf("expected Amount=9900, got %d", txn.Amount.Amount)
	}
	if txn.DiscountAmount == nil || txn.DiscountAmount.Amount != 1000 {
		t.Errorf("expected DiscountAmount=1000, got %v", txn.DiscountAmount)
	}
	if txn.TaxAmount == nil || txn.TaxAmount.Amount != 900 {
		t.Errorf("expected TaxAmount=900, got %v", txn.TaxAmount)
	}
	if txn.CouponID != "cpn-1" {
		t.Errorf("expected CouponID=cpn-1, got %s", txn.CouponID)
	}
	if txn.Status != model.StatusAuthorized {
		t.Errorf("expected AUTHORIZED, got %s", txn.Status)
	}
}

func TestPurchase_WithoutCoupon_UsesCatalogPricePlusTax(t *testing.T) {
	txnRepo := &spyTxnRepo{}
	catalogQ := &stubCatalog{
		product: &port.ProductView{
			ID: "p1", Name: "Product 1", Amount: 10000, Currency: "USD", IsActive: true,
		},
	}
	gw := &stubGateway{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "ref-2", AuthCode: "auth-2"},
	}
	taxQ := &stubTaxQuery{bp: 1000}

	merchantQ := &stubMerchantQuery{cred: activeMerchantCred()}
	factory := &stubGatewayFactory{cardGateway: gw, paypalGateway: &stubNoOpPayPalGateway{}}
	uc := application.NewChargeUseCase(merchantQ, factory, txnRepo, catalogQ, &stubCardQuery{}, nil, taxQ)

	txn, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "m1", UserID: "u1", ProductID: "p1",
		Token: model.CardToken{TokenID: "tok-2", Last4: "1234", Brand: "MC"},
	})

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	// 原价 10000, 无折扣, 税 10%=1000, 最终 11000
	if txn.Amount.Amount != 11000 {
		t.Errorf("expected Amount=11000, got %d", txn.Amount.Amount)
	}
}

func TestPurchase_NoCouponNoTax_UsesCatalogPrice(t *testing.T) {
	txnRepo := &spyTxnRepo{}
	catalogQ := &stubCatalog{
		product: &port.ProductView{
			ID: "p1", Name: "Product 1", Amount: 10000, Currency: "USD", IsActive: true,
		},
	}
	gw := &stubGateway{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "ref-3", AuthCode: "auth-3"},
	}

	merchantQ := &stubMerchantQuery{cred: activeMerchantCred()}
	factory := &stubGatewayFactory{cardGateway: gw, paypalGateway: &stubNoOpPayPalGateway{}}
	uc := application.NewChargeUseCase(merchantQ, factory, txnRepo, catalogQ, &stubCardQuery{}, nil, nil)

	txn, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "m1", UserID: "u1", ProductID: "p1",
		Token: model.CardToken{TokenID: "tok-3", Last4: "5678", Brand: "VISA"},
	})

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if txn.Amount.Amount != 10000 {
		t.Errorf("expected Amount=10000, got %d", txn.Amount.Amount)
	}
	if txn.DiscountAmount != nil {
		t.Errorf("expected nil DiscountAmount, got %v", txn.DiscountAmount)
	}
	if txn.TaxAmount != nil {
		t.Errorf("expected nil TaxAmount, got %v", txn.TaxAmount)
	}
}

func TestPurchase_CouponNotFound_ReturnsError(t *testing.T) {
	txnRepo := &spyTxnRepo{}
	catalogQ := &stubCatalog{
		product: &port.ProductView{ID: "p1", Amount: 10000, Currency: "USD", IsActive: true},
	}
	gw := &stubGateway{authorizeResult: &port.GatewayAuthResult{ProviderRef: "ref-4"}}
	couponApplier := &stubCouponApplier{applyErr: couponModel.ErrCouponNotFound}

	merchantQ := &stubMerchantQuery{cred: activeMerchantCred()}
	factory := &stubGatewayFactory{cardGateway: gw, paypalGateway: &stubNoOpPayPalGateway{}}
	uc := application.NewChargeUseCase(merchantQ, factory, txnRepo, catalogQ, &stubCardQuery{}, couponApplier, nil)

	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "m1", UserID: "u1", ProductID: "p1",
		Token: model.CardToken{TokenID: "tok-4"}, CouponCode: "INVALID",
	})

	if err != couponModel.ErrCouponNotFound {
		t.Errorf("expected ErrCouponNotFound, got %v", err)
	}
	if txnRepo.saveCalls != 0 {
		t.Errorf("expected Save NOT called, got %d", txnRepo.saveCalls)
	}
}

func TestPurchase_AuthFailed_RollbacksCoupon(t *testing.T) {
	txnRepo := &spyTxnRepo{}
	catalogQ := &stubCatalog{
		product: &port.ProductView{ID: "p1", Amount: 10000, Currency: "USD", IsActive: true},
	}
	gw := &stubGateway{authorizeErr: errors.New("declined")}
	couponApplier := &stubCouponApplier{
		applied: &port.AppliedCoupon{CouponID: "cpn-2", DiscountType: "FIXED", DiscountValue: 500},
	}

	merchantQ := &stubMerchantQuery{cred: activeMerchantCred()}
	factory := &stubGatewayFactory{cardGateway: gw, paypalGateway: &stubNoOpPayPalGateway{}}
	uc := application.NewChargeUseCase(merchantQ, factory, txnRepo, catalogQ, &stubCardQuery{}, couponApplier, nil)

	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "m1", UserID: "u1", ProductID: "p1",
		Token: model.CardToken{TokenID: "tok-5"}, CouponCode: "FIXED5",
	})

	if err != model.ErrAuthorizationDeclined {
		t.Errorf("expected ErrAuthorizationDeclined, got %v", err)
	}
	if couponApplier.rollbackCalls != 1 {
		t.Errorf("expected 1 rollback call, got %d", couponApplier.rollbackCalls)
	}
}
