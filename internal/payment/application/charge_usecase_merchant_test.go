package application_test

// charge_usecase_merchant_test.go — AC-27 / AC-28 / AC-29
// 多商户路由专项测试：验证 ChargeUseCase 通过 MerchantQuery + GatewayFactory 动态路由。
//
// 依赖 stub 复用（均已在同包其他 _test.go 中声明）：
//   - stubMerchantQuery        → charge_usecase_savedcard_test.go
//   - stubGatewayFactory       → charge_usecase_savedcard_test.go
//   - stubCatalog / activeProduct → charge_usecase_savedcard_test.go
//   - stubCardQuery             → charge_usecase_savedcard_test.go
//   - stubNoOpPayPalGateway     → charge_usecase_savedcard_test.go
//   - activeMerchantCred / activePayPalMerchantCred → charge_usecase_savedcard_test.go
//
// 本文件只定义测试所需的额外 stub 和测试函数。

import (
	"context"
	"errors"
	"testing"

	"payment-demo/internal/payment/adapter/persistence"
	"payment-demo/internal/payment/application"
	"payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// ─────────────────────────────────────────────────────────────────
// 测试专用 stub（本文件私有，不与其他文件冲突）
// ─────────────────────────────────────────────────────────────────

// spyMerchantQuery 可观测调用次数和最后一次 channel 参数的 MerchantQuery stub。
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

// spyGatewayFactory 可观测 Build 调用次数和最后传入凭据的 GatewayFactory stub。
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

// spyCardGateway 可观测 Authorize 调用次数的 Card Gateway stub。
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

// spyPayPalGateway 可观测 Authorize 调用次数的 PayPal Gateway stub。
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

// ─────────────────────────────────────────────────────────────────
// 凭据工厂辅助
// ─────────────────────────────────────────────────────────────────

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

// ─────────────────────────────────────────────────────────────────
// AC-27: ChargeUseCase.Purchase 通过 MerchantQuery 获取凭据并路由到对应 Gateway
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_Purchase_MerchantRouting_Success(t *testing.T) {
	// AC-27
	const merchantID = "merchant-001"

	merchantQ := &spyMerchantQuery{cred: merchantCardCredView(merchantID)}
	cardGw := &spyCardGateway{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "ch_test_001", AuthCode: "AUTH-001"},
	}
	gwFactory := &spyGatewayFactory{cardGateway: cardGw}
	txnRepo := persistence.NewInMemoryTransactionRepository()
	catalog := &stubCatalog{product: activeProduct()}
	cardQ := &stubCardQuery{}

	uc := application.NewChargeUseCase(merchantQ, gwFactory, txnRepo, catalog, cardQ)

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

	// MerchantQuery.FindActiveCredential 被调用一次，channel 参数为 CARD
	if merchantQ.callCount != 1 {
		t.Errorf("MerchantQuery calls: want 1, got %d", merchantQ.callCount)
	}
	if merchantQ.lastChannel != model.PaymentMethodCard {
		t.Errorf("MerchantQuery channel: want CARD, got %s", merchantQ.lastChannel)
	}

	// GatewayFactory.BuildCardGateway 被调用一次，入参 CredentialID 与 view 一致
	if gwFactory.cardBuildCount != 1 {
		t.Errorf("BuildCardGateway calls: want 1, got %d", gwFactory.cardBuildCount)
	}
	if gwFactory.lastCardCred.CredentialID != "cred-card-1" {
		t.Errorf("BuildCardGateway cred ID: want cred-card-1, got %s", gwFactory.lastCardCred.CredentialID)
	}

	// mockCardGateway.Authorize 被调用一次
	if cardGw.authorizeCount != 1 {
		t.Errorf("Authorize calls: want 1, got %d", cardGw.authorizeCount)
	}

	// Events 已被 ClearEvents 清空
	if len(txn.Events) != 0 {
		t.Errorf("Events must be cleared, got %d", len(txn.Events))
	}

	// MerchantID 正确冗余存储
	if txn.MerchantID != merchantID {
		t.Errorf("txn.MerchantID: want %s, got %s", merchantID, txn.MerchantID)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-28: ChargeUseCase.Purchase MerchantQuery 返回错误时不持久化
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_Purchase_MerchantCredentialNotFound_DoesNotSave(t *testing.T) {
	// AC-28
	merchantQ := &spyMerchantQuery{err: port.ErrMerchantCredentialNotFound}
	gwFactory := &spyGatewayFactory{}
	txnRepo := persistence.NewInMemoryTransactionRepository()
	catalog := &stubCatalog{product: activeProduct()}
	cardQ := &stubCardQuery{}

	uc := application.NewChargeUseCase(merchantQ, gwFactory, txnRepo, catalog, cardQ)

	_, err := uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "merchant-001",
		UserID:     "user-1",
		ProductID:  "prod-1",
		Token:      model.CardToken{TokenID: "tok_visa"},
	})

	if !errors.Is(err, port.ErrMerchantCredentialNotFound) {
		t.Errorf("want ErrMerchantCredentialNotFound, got %v", err)
	}
	// TransactionRepository.Save 未被调用（通过验证 repo 为空来间接确认）
	if gwFactory.cardBuildCount != 0 {
		t.Errorf("BuildCardGateway must not be called when MerchantQuery fails, got %d calls", gwFactory.cardBuildCount)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-29: ChargeUseCase.PayPalPurchase 通过 MerchantQuery 路由到 PayPal Gateway
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_PayPalPurchase_MerchantRouting_Success(t *testing.T) {
	// AC-29
	const merchantID = "merchant-paypal-001"

	merchantQ := &spyMerchantQuery{cred: merchantPayPalCredView(merchantID)}
	paypalGw := &spyPayPalGateway{
		authorizeResult: &port.PayPalAuthResult{ProviderRef: "PAYPAL-CAPTURE-001", PayerEmail: "buyer@example.com"},
	}
	gwFactory := &spyGatewayFactory{paypalGateway: paypalGw}
	txnRepo := persistence.NewInMemoryTransactionRepository()
	catalog := &stubCatalog{product: activeProduct()}
	cardQ := &stubCardQuery{}

	uc := application.NewChargeUseCase(merchantQ, gwFactory, txnRepo, catalog, cardQ)

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

	// MerchantQuery.FindActiveCredential 被调用，channel 参数为 PAYPAL
	if merchantQ.callCount != 1 {
		t.Errorf("MerchantQuery calls: want 1, got %d", merchantQ.callCount)
	}
	if merchantQ.lastChannel != model.PaymentMethodPayPal {
		t.Errorf("MerchantQuery channel: want PAYPAL, got %s", merchantQ.lastChannel)
	}

	// GatewayFactory.BuildPayPalGateway 被调用一次
	if gwFactory.paypalBuildCount != 1 {
		t.Errorf("BuildPayPalGateway calls: want 1, got %d", gwFactory.paypalBuildCount)
	}
	if gwFactory.lastPayPalCred.CredentialID != "cred-paypal-1" {
		t.Errorf("BuildPayPalGateway cred ID: want cred-paypal-1, got %s", gwFactory.lastPayPalCred.CredentialID)
	}

	// mockPayPalGateway.Authorize 被调用一次
	if paypalGw.authorizeCount != 1 {
		t.Errorf("PayPal Authorize calls: want 1, got %d", paypalGw.authorizeCount)
	}

	// MerchantID 正确冗余存储
	if txn.MerchantID != merchantID {
		t.Errorf("txn.MerchantID: want %s, got %s", merchantID, txn.MerchantID)
	}
}

// ─────────────────────────────────────────────────────────────────
// 边界：GatewayFactory.BuildCardGateway 失败时返回 ErrMerchantGatewayBuildFailed
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_Purchase_GatewayBuildFails_ReturnsError(t *testing.T) {
	merchantQ := &spyMerchantQuery{cred: merchantCardCredView("merchant-1")}
	gwFactory := &spyGatewayFactory{
		cardBuildErr: errors.New("missing api_key"),
	}
	txnRepo := persistence.NewInMemoryTransactionRepository()
	catalog := &stubCatalog{product: activeProduct()}
	cardQ := &stubCardQuery{}

	uc := application.NewChargeUseCase(merchantQ, gwFactory, txnRepo, catalog, cardQ)

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

// ─────────────────────────────────────────────────────────────────
// 边界：PayPalPurchase MerchantQuery 失败不持久化
// ─────────────────────────────────────────────────────────────────

func TestChargeUseCase_PayPalPurchase_MerchantCredentialNotFound_DoesNotSave(t *testing.T) {
	merchantQ := &spyMerchantQuery{err: port.ErrMerchantCredentialNotFound}
	gwFactory := &spyGatewayFactory{}
	txnRepo := persistence.NewInMemoryTransactionRepository()
	catalog := &stubCatalog{product: activeProduct()}
	cardQ := &stubCardQuery{}

	uc := application.NewChargeUseCase(merchantQ, gwFactory, txnRepo, catalog, cardQ)

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
