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
// Test Doubles
// ─────────────────────────────────────────────────────────────────

// stubGateway 可控制 Authorize 返回
type stubGateway struct {
	authorizeResult *port.GatewayAuthResult
	authorizeErr    error
	authorizedWith  *paymentModel.CardToken // 记录 Authorize 收到的 CardToken
	authorizeCalled bool
}

func (g *stubGateway) Authorize(_ context.Context, token paymentModel.CardToken, _ paymentModel.Money) (*port.GatewayAuthResult, error) {
	g.authorizeCalled = true
	g.authorizedWith = &token
	return g.authorizeResult, g.authorizeErr
}

func (g *stubGateway) Capture(_ context.Context, _ string, _ paymentModel.Money) error { return nil }
func (g *stubGateway) Refund(_ context.Context, _ string, _ paymentModel.Money) error  { return nil }

// stubCatalog 返回固定商品
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

// stubCardQuery 实现 port.CardQuery
type stubCardQuery struct {
	view *port.SavedCardView
	err  error
}

func (q *stubCardQuery) FindActiveCard(_ context.Context, _ string) (*port.SavedCardView, error) {
	return q.view, q.err
}

// stubNoOpPayPalGateway 占位 PayPal Gateway，Card 测试不会调用它
type stubNoOpPayPalGateway struct{}

func (g *stubNoOpPayPalGateway) Authorize(_ context.Context, _ paymentModel.PayPalToken, _ paymentModel.Money) (*port.PayPalAuthResult, error) {
	return nil, nil
}
func (g *stubNoOpPayPalGateway) Capture(_ context.Context, _ string, _ paymentModel.Money) error {
	return nil
}
func (g *stubNoOpPayPalGateway) Refund(_ context.Context, _ string, _ paymentModel.Money) error {
	return nil
}

// ─────────────────────────────────────────────────────────────────
// 多商户测试桩：stubMerchantQuery + stubGatewayFactory
// 这两个 stub 在所有 application_test 用例中共用
// ─────────────────────────────────────────────────────────────────

// stubMerchantQuery 可控制 FindActiveCredential 返回
type stubMerchantQuery struct {
	cred *port.ChannelCredentialView
	err  error
}

func (m *stubMerchantQuery) FindActiveCredential(_ context.Context, _ string, _ paymentModel.PaymentMethod) (*port.ChannelCredentialView, error) {
	return m.cred, m.err
}

// activeMerchantCred 返回用于 Card 渠道的测试凭据
func activeMerchantCred() *port.ChannelCredentialView {
	return &port.ChannelCredentialView{
		CredentialID: "cred-1",
		MerchantID:   "merchant-1",
		Channel:      "CARD",
		Secrets:      map[string]string{"api_key": "sk_test_xxx"},
	}
}

// activePayPalMerchantCred 返回用于 PayPal 渠道的测试凭据
func activePayPalMerchantCred() *port.ChannelCredentialView {
	return &port.ChannelCredentialView{
		CredentialID: "cred-2",
		MerchantID:   "merchant-1",
		Channel:      "PAYPAL",
		Secrets:      map[string]string{"client_id": "cl_xxx", "client_secret": "sec_xxx"},
	}
}

// stubGatewayFactory 根据渠道返回对应的 stub Gateway
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

// ─────────────────────────────────────────────────────────────────
// 辅助：组装 ChargeUseCase（Card 测试用）
// ─────────────────────────────────────────────────────────────────

// buildChargeUseCase 构建用于 Card 支付测试的 UseCase。
// merchantQuery 提供固定商户凭据（CARD 渠道），gatewayFactory 返回传入的 cardGw。
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
	return application.NewChargeUseCase(merchantQ, factory, repo, catalog, cardQuery)
}

// ─────────────────────────────────────────────────────────────────
// AC-33  使用 SavedCardID 成功发起支付
// ─────────────────────────────────────────────────────────────────

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

	// 应调用 Authorize，且 CardToken.TokenID == VaultToken
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

	// 交易状态应为 AUTHORIZED
	if txn.Status != paymentModel.StatusAuthorized {
		t.Errorf("want AUTHORIZED, got %s", txn.Status)
	}
	// events 已被 publishEvents 清空
	if len(txn.Events) != 0 {
		t.Errorf("events should be cleared, got %d", len(txn.Events))
	}
}

// AC-34  使用 Suspended 已保存卡发起支付被拒绝
func TestChargeUseCase_Purchase_WithSuspendedSavedCard_ReturnsError(t *testing.T) {
	gw := &stubGateway{}
	catalog := &stubCatalog{product: activeProduct()}
	// CardQuery 返回 IsActive=false（Suspended 卡）
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
	if !errors.Is(err, paymentModel.ErrCardNotUsable) {
		t.Errorf("want ErrCardNotUsable, got %v", err)
	}
	if gw.authorizeCalled {
		t.Error("Authorize must NOT be called for inactive card")
	}
}

// 使用不存在的 SavedCardID 时 CardQuery 返回 error → 购买中止
func TestChargeUseCase_Purchase_CardQueryFails_ReturnsError(t *testing.T) {
	gw := &stubGateway{}
	catalog := &stubCatalog{product: activeProduct()}
	cardQuery := &stubCardQuery{
		err: paymentModel.ErrCardNotFound,
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

// 卡归属他人时 Purchase 返回 ErrCardNotFound（UserID 不匹配）
func TestChargeUseCase_Purchase_CardBelongsToOtherUser_ReturnsError(t *testing.T) {
	gw := &stubGateway{}
	catalog := &stubCatalog{product: activeProduct()}
	// 卡的 UserID 与请求 UserID 不同
	cardQuery := &stubCardQuery{
		view: &port.SavedCardView{
			CardID:   "card-1",
			UserID:   "other-user", // 不匹配
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

// 无 SavedCardID 时仍走原有 one-time Token 流程
func TestChargeUseCase_Purchase_WithOneTimeToken_DoesNotCallCardQuery(t *testing.T) {
	gw := &stubGateway{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "pi_002", AuthCode: "AUTH_002"},
	}
	catalog := &stubCatalog{product: activeProduct()}

	// 用 spy 检测 CardQuery 是否被调用
	spy := &spyCardQuery{}
	uc := buildChargeUseCase(gw, catalog, spy)
	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      paymentModel.CardToken{TokenID: "tok_onetime", Last4: "1111", Brand: "Mastercard"},
		// SavedCardID 为空
	})
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if spy.called {
		t.Error("CardQuery.FindActiveCard must NOT be called when SavedCardID is empty")
	}
}

type spyCardQuery struct {
	called bool
}

func (q *spyCardQuery) FindActiveCard(_ context.Context, _ string) (*port.SavedCardView, error) {
	q.called = true
	return nil, nil
}

// ─────────────────────────────────────────────────────────────────
// 多商户专项：Purchase 时 merchant_id 为空 → ErrMerchantRequired
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_Purchase_MissingMerchantID_ReturnsError(t *testing.T) {
	gw := &stubGateway{}
	catalog := &stubCatalog{product: activeProduct()}
	uc := buildChargeUseCase(gw, catalog, &stubCardQuery{})

	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "", // 缺失
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      paymentModel.CardToken{TokenID: "tok_xxx"},
	})
	if !errors.Is(err, paymentModel.ErrMerchantRequired) {
		t.Errorf("want ErrMerchantRequired, got %v", err)
	}
	if gw.authorizeCalled {
		t.Error("Authorize must NOT be called when MerchantID is missing")
	}
}

// 多商户专项：MerchantQuery 返回凭据不存在 → Purchase 中止
func TestChargeUseCase_Purchase_MerchantCredentialNotFound_ReturnsError(t *testing.T) {
	gw := &stubGateway{}
	repo := persistence.NewInMemoryTransactionRepository()
	merchantQ := &stubMerchantQuery{err: port.ErrMerchantCredentialNotFound}
	factory := &stubGatewayFactory{cardGateway: gw, paypalGateway: &stubNoOpPayPalGateway{}}
	uc := application.NewChargeUseCase(merchantQ, factory, repo, &stubCatalog{product: activeProduct()}, &stubCardQuery{})

	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "merchant-unknown",
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      paymentModel.CardToken{TokenID: "tok_xxx"},
	})
	if !errors.Is(err, port.ErrMerchantCredentialNotFound) {
		t.Errorf("want ErrMerchantCredentialNotFound, got %v", err)
	}
	if gw.authorizeCalled {
		t.Error("Authorize must NOT be called when credential is not found")
	}
}
