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

// ─────────────────────────────────────────────────────────────────
// 辅助：组装 ChargeUseCase
// ─────────────────────────────────────────────────────────────────

func buildChargeUseCase(
	gw port.PaymentGateway,
	catalog port.CatalogQuery,
	cardQuery port.CardQuery,
) *application.ChargeUseCase {
	repo := persistence.NewInMemoryTransactionRepository()
	return application.NewChargeUseCase(gw, repo, catalog, cardQuery)
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
	var cardQueryCalled bool
	cardQuery := &stubCardQuery{}
	// 用 wrapper 检测是否被调用
	_ = cardQuery // 下面用 stubCardQuerySpy 更简洁

	// 直接用 spy 结构
	spy := &spyCardQuery{}
	uc := buildChargeUseCase(gw, catalog, spy)
	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		UserID:    "user-1",
		ProductID: "prod-1",
		Token:     paymentModel.CardToken{TokenID: "tok_onetime", Last4: "1111", Brand: "Mastercard"},
		// SavedCardID 为空
	})
	_ = cardQueryCalled
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
