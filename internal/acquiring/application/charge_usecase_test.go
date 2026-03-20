package application_test

import (
	"context"
	"errors"
	"testing"

	"payment-demo/internal/acquiring/adapter/persistence"
	"payment-demo/internal/acquiring/application"
	"payment-demo/internal/acquiring/domain/model"
	"payment-demo/internal/acquiring/domain/port"
)

// ═══════════════════════════════════════════════════════════════════
// Shared Stubs — 所有 application_test 文件共用
// ═══════════════════════════════════════════════════════════════════

// --- Card Gateway ---

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

// --- Merchant helpers ---

func seededMerchantRepo(merchants ...*model.Merchant) *stubMerchantRepo {
	repo := newStubRepo()
	for _, m := range merchants {
		repo.store[m.ID] = m
	}
	return repo
}

func activeMerchantWithCard(merchantID string) *model.Merchant {
	return &model.Merchant{
		ID:     model.MerchantID(merchantID),
		Status: model.MerchantStatusActive,
		Credentials: []model.ChannelCredential{
			{
				ID:      model.ChannelCredentialID("cred-card-1"),
				Channel: model.PaymentMethodCard,
				Secrets: map[string]string{"api_key": "sk_live_xxx"},
				Status:  model.CredentialStatusActive,
			},
		},
	}
}

func defaultActiveMerchant() *model.Merchant {
	return activeMerchantWithCard("merchant-1")
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

// --- CardCommand ---

type stubCardCommand struct{}

func (c *stubCardCommand) StoreChannelToken(_ context.Context, _, _, _, _ string) error {
	return nil
}
func (c *stubCardCommand) BindCardFromToken(_ context.Context, _ port.BindFromTokenCommand) (string, error) {
	return "card-new", nil
}
func (c *stubCardCommand) PrepareOneTimeToken(_ context.Context, _, _ string) (string, error) {
	return "ct_onetime_xxx", nil
}

func (c *stubCardCommand) ResolveCardForGateway(_ context.Context, _, _ string) (*port.ResolvedCard, error) {
	return &port.ResolvedCard{Last4: "4242", Brand: "visa", GatewayToken: "ct_gw_stub"}, nil
}

// --- UseCase 组装辅助 ---

func buildChargeUseCase(
	cardGw port.PaymentGateway,
	cardQuery port.CardQuery,
) *application.ChargeUseCase {
	repo := persistence.NewInMemoryTransactionRepository()
	merchantRepo := seededMerchantRepo(defaultActiveMerchant())
	factory := &stubGatewayFactory{
		cardGateway:   cardGw,
		paypalGateway: &stubNoOpPayPalGateway{},
	}
	return application.NewChargeUseCase(merchantRepo, factory, repo, cardQuery, &stubCardCommand{})
}

// --- Spy stubs（商户路由验证用）---

type spyMerchantRepo struct {
	merchant  *model.Merchant
	err       error
	callCount int
}

func (s *spyMerchantRepo) Save(_ context.Context, _ *model.Merchant) error { return nil }
func (s *spyMerchantRepo) FindByID(_ context.Context, _ model.MerchantID) (*model.Merchant, error) {
	s.callCount++
	if s.err != nil {
		return nil, s.err
	}
	return s.merchant, nil
}
func (s *spyMerchantRepo) FindAll(_ context.Context) ([]*model.Merchant, error) { return nil, nil }

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

func merchantWithPayPalCred(merchantID string) *model.Merchant {
	return &model.Merchant{
		ID:     model.MerchantID(merchantID),
		Status: model.MerchantStatusActive,
		Credentials: []model.ChannelCredential{
			{
				ID:      model.ChannelCredentialID("cred-paypal-1"),
				Channel: model.PaymentMethodPayPal,
				Secrets: map[string]string{"client_id": "id", "client_secret": "sec"},
				Status:  model.CredentialStatusActive,
			},
		},
	}
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

func (s *spyTxnRepo) FindByProviderRef(_ context.Context, ref string) (*model.PaymentTransaction, error) {
	if s.savedTxn != nil && s.savedTxn.ProviderRef == ref {
		return s.savedTxn, nil
	}
	return nil, model.ErrTransactionNotFound
}

// ═══════════════════════════════════════════════════════════════════
// Card Purchase — SavedCard 测试
// ═══════════════════════════════════════════════════════════════════

func TestChargeUseCase_Purchase_WithSavedCard_Succeeds(t *testing.T) {
	gw := &stubGateway{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "pi_001", AuthCode: "AUTH_001"},
	}
	cardQuery := &stubCardQuery{
		view: &port.SavedCardView{
			CardID:        "card-1",
			UserID:        "user-1",
			ChannelTokens: map[string]string{"CARD": "tok_saved"},
			Last4:         "4242",
			Brand:         "Visa",
			IsActive:      true,
		},
	}

	uc := buildChargeUseCase(gw, cardQuery)
	txn, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID:  "merchant-1",
		UserID:      "user-1",
		OrderID:     "order-1",
		Amount:      model.NewMoney(1000, "USD"),
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
	cardQuery := &stubCardQuery{
		view: &port.SavedCardView{
			CardID:        "card-sus",
			UserID:        "user-1",
			ChannelTokens: map[string]string{"CARD": "tok_sus"},
			IsActive:      false,
		},
	}

	uc := buildChargeUseCase(gw, cardQuery)
	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID:  "merchant-1",
		UserID:      "user-1",
		OrderID:     "order-1",
		Amount:      model.NewMoney(1000, "USD"),
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
	cardQuery := &stubCardQuery{
		err: model.ErrCardNotFound,
	}

	uc := buildChargeUseCase(gw, cardQuery)
	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID:  "merchant-1",
		UserID:      "user-1",
		OrderID:     "order-1",
		Amount:      model.NewMoney(1000, "USD"),
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
	cardQuery := &stubCardQuery{
		view: &port.SavedCardView{
			CardID:        "card-1",
			UserID:        "other-user",
			ChannelTokens: map[string]string{"CARD": "tok_abc"},
			IsActive:      true,
		},
	}

	uc := buildChargeUseCase(gw, cardQuery)
	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID:  "merchant-1",
		UserID:      "user-1",
		OrderID:     "order-1",
		Amount:      model.NewMoney(1000, "USD"),
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

	spy := &spyCardQuery{}
	uc := buildChargeUseCase(gw, spy)
	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "user-1",
		OrderID:    "order-1",
		Amount:     model.NewMoney(1000, "USD"),
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
	uc := buildChargeUseCase(gw, &stubCardQuery{})

	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "",
		UserID:     "user-1",
		OrderID:    "order-1",
		Amount:     model.NewMoney(1000, "USD"),
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
	merchantRepo := newStubRepo()
	merchantRepo.findErr = model.ErrMerchantNotFound
	factory := &stubGatewayFactory{cardGateway: gw, paypalGateway: &stubNoOpPayPalGateway{}}
	uc := application.NewChargeUseCase(merchantRepo, factory, repo, &stubCardQuery{}, &stubCardCommand{})

	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "merchant-unknown",
		UserID:     "user-1",
		OrderID:    "order-1",
		Amount:     model.NewMoney(1000, "USD"),
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

	merchantRepo := &spyMerchantRepo{merchant: activeMerchantWithCard(merchantID)}
	cardGw := &spyCardGateway{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "ch_test_001", AuthCode: "AUTH-001"},
	}
	gwFactory := &spyGatewayFactory{cardGateway: cardGw}
	txnRepo := persistence.NewInMemoryTransactionRepository()
	cardQ := &stubCardQuery{}

	uc := application.NewChargeUseCase(merchantRepo, gwFactory, txnRepo, cardQ, &stubCardCommand{})

	txn, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: merchantID,
		UserID:     "user-1",
		OrderID:    "order-1",
		Amount:     model.NewMoney(1000, "USD"),
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
	if merchantRepo.callCount != 1 {
		t.Errorf("MerchantRepo calls: want 1, got %d", merchantRepo.callCount)
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

func TestChargeUseCase_Purchase_CT_Token_AuthorizesWithGatewayToken_KeepsOriginalInTxn(t *testing.T) {
	merchantRepo := seededMerchantRepo(defaultActiveMerchant())
	gw := &stubGateway{
		authorizeResult: &port.GatewayAuthResult{
			ProviderRef: "ch_1", AuthCode: "A1", Channel: "stripe", RecurringToken: "rt_1",
		},
	}
	factory := &stubGatewayFactory{cardGateway: gw, paypalGateway: &stubNoOpPayPalGateway{}}
	txnRepo := persistence.NewInMemoryTransactionRepository()

	uc := application.NewChargeUseCase(merchantRepo, factory, txnRepo, &stubCardQuery{}, &stubCardCommand{})

	txn, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "user-1",
		OrderID:    "order-1",
		Amount:     model.NewMoney(1000, "USD"),
		Token:      model.CardToken{TokenID: "ct_from_client", Last4: "1111", Brand: "wrong"},
	})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	if !gw.authorizeCalled {
		t.Fatal("Authorize was not called")
	}
	if gw.authorizedWith == nil || gw.authorizedWith.TokenID != "ct_gw_stub" {
		t.Fatalf("Authorize TokenID: want ct_gw_stub, got %#v", gw.authorizedWith)
	}
	if txn.CardToken.TokenID != "ct_from_client" {
		t.Errorf("txn.CardToken.TokenID: want original ct_from_client, got %s", txn.CardToken.TokenID)
	}
	if txn.CardToken.Last4 != "4242" || txn.CardToken.Brand != "visa" {
		t.Errorf("txn.CardToken: want last4=4242 brand=visa, got %+v", txn.CardToken)
	}
}

func TestChargeUseCase_Purchase_MerchantCredentialNotFound_DoesNotSave(t *testing.T) {
	merchantRepo := &spyMerchantRepo{err: model.ErrMerchantNotFound}
	gwFactory := &spyGatewayFactory{}
	txnRepo := persistence.NewInMemoryTransactionRepository()
	cardQ := &stubCardQuery{}

	uc := application.NewChargeUseCase(merchantRepo, gwFactory, txnRepo, cardQ, &stubCardCommand{})

	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "merchant-001",
		UserID:     "user-1",
		OrderID:    "order-1",
		Amount:     model.NewMoney(1000, "USD"),
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
	merchantRepo := &spyMerchantRepo{merchant: activeMerchantWithCard("merchant-1")}
	gwFactory := &spyGatewayFactory{
		cardBuildErr: errors.New("missing api_key"),
	}
	txnRepo := persistence.NewInMemoryTransactionRepository()
	cardQ := &stubCardQuery{}

	uc := application.NewChargeUseCase(merchantRepo, gwFactory, txnRepo, cardQ, &stubCardCommand{})

	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "user-1",
		OrderID:    "order-1",
		Amount:     model.NewMoney(1000, "USD"),
		Token:      model.CardToken{TokenID: "tok_visa"},
	})

	if !errors.Is(err, model.ErrMerchantGatewayBuildFailed) {
		t.Errorf("want ErrMerchantGatewayBuildFailed, got %v", err)
	}
}
