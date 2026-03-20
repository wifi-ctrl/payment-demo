package application_test

import (
	"context"
	"errors"
	"testing"

	"payment-demo/internal/order/application"
	"payment-demo/internal/order/domain/model"
	"payment-demo/internal/order/domain/port"
	"payment-demo/internal/shared/money"
)

// ── Stubs ──────────────────────────────────────────────────────

type stubOrderRepo struct {
	orders map[model.OrderID]*model.Order
}

func newStubOrderRepo() *stubOrderRepo {
	return &stubOrderRepo{orders: make(map[model.OrderID]*model.Order)}
}

func (r *stubOrderRepo) Save(_ context.Context, o *model.Order) error {
	r.orders[o.ID] = o
	return nil
}

func (r *stubOrderRepo) FindByID(_ context.Context, id model.OrderID) (*model.Order, error) {
	o, ok := r.orders[id]
	if !ok {
		return nil, model.ErrOrderNotFound
	}
	return o, nil
}

type stubCatalog struct {
	product *port.ProductView
	err     error
}

func (c *stubCatalog) FindProduct(_ context.Context, _ string) (*port.ProductView, error) {
	return c.product, c.err
}

type stubCoupon struct {
	applied  *port.AppliedCoupon
	err      error
	rollback int
}

func (c *stubCoupon) Apply(_ context.Context, _ string, _ string) (*port.AppliedCoupon, error) {
	return c.applied, c.err
}

func (c *stubCoupon) Rollback(_ context.Context, _ string) error {
	c.rollback++
	return nil
}

type stubTax struct {
	bp  int64
	err error
}

func (t *stubTax) FindTaxRate(_ context.Context, _ string, _ string) (int64, error) {
	return t.bp, t.err
}

type stubPayment struct {
	chargeResult *port.ChargeResult
	chargeErr    error
	captureErr   error
	refundErr    error
}

func (p *stubPayment) Charge(_ context.Context, _ port.ChargeRequest) (*port.ChargeResult, error) {
	return p.chargeResult, p.chargeErr
}

func (p *stubPayment) Capture(_ context.Context, _, _ string) error {
	return p.captureErr
}

func (p *stubPayment) Refund(_ context.Context, _, _ string) error {
	return p.refundErr
}

func activeProduct() *port.ProductView {
	return &port.ProductView{
		ID: "prod-1", Name: "Widget",
		Amount: 10000, Currency: "USD",
		IsActive: true,
	}
}

func buildUseCase(
	catalog port.CatalogQuery,
	coupon port.CouponApplier,
	tax port.TaxRateQuery,
	payment port.PaymentCommand,
) (*application.OrderUseCase, *stubOrderRepo) {
	repo := newStubOrderRepo()
	uc := application.NewOrderUseCase(repo, catalog, coupon, tax, payment)
	return uc, repo
}

// ── Tests ──────────────────────────────────────────────────────

func TestCreateOrder_Success(t *testing.T) {
	payment := &stubPayment{
		chargeResult: &port.ChargeResult{TransactionID: "txn-1", Status: "AUTHORIZED"},
	}
	uc, _ := buildUseCase(
		&stubCatalog{product: activeProduct()},
		nil,
		&stubTax{bp: 1000},
		payment,
	)

	order, err := uc.CreateOrder(context.Background(), application.CreateOrderRequest{
		MerchantID: "m1", UserID: "u1", ProductID: "prod-1",
	}, application.PaymentDetail{
		CardToken: "ct_xxx", CardLast4: "4242", CardBrand: "visa",
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != model.OrderStatusAuthorized {
		t.Errorf("want AUTHORIZED, got %s", order.Status)
	}
	if order.TransactionID != "txn-1" {
		t.Errorf("want txn-1, got %s", order.TransactionID)
	}
	// 原价 10000, 无折扣, 税 10%=1000, 最终 11000
	if order.Price.FinalAmount.Amount != 11000 {
		t.Errorf("want 11000, got %d", order.Price.FinalAmount.Amount)
	}
}

func TestCreateOrder_WithCoupon(t *testing.T) {
	payment := &stubPayment{
		chargeResult: &port.ChargeResult{TransactionID: "txn-2", Status: "AUTHORIZED"},
	}
	coupon := &stubCoupon{
		applied: &port.AppliedCoupon{CouponID: "cpn-1", DiscountType: "PERCENTAGE", DiscountValue: 1000},
	}
	uc, _ := buildUseCase(
		&stubCatalog{product: activeProduct()},
		coupon,
		&stubTax{bp: 1000},
		payment,
	)

	order, err := uc.CreateOrder(context.Background(), application.CreateOrderRequest{
		MerchantID: "m1", UserID: "u1", ProductID: "prod-1",
		CouponCode: "SAVE10",
	}, application.PaymentDetail{CardToken: "ct_xxx"})
	if err != nil {
		t.Fatal(err)
	}
	// 原价 10000, 折扣 10%=1000, 折后 9000, 税 10%=900, 最终 9900
	if order.Price.FinalAmount.Amount != 9900 {
		t.Errorf("want 9900, got %d", order.Price.FinalAmount.Amount)
	}
	if order.Price.DiscountAmount.Amount != 1000 {
		t.Errorf("want discount 1000, got %d", order.Price.DiscountAmount.Amount)
	}
	if order.CouponID != "cpn-1" {
		t.Errorf("want cpn-1, got %s", order.CouponID)
	}
}

func TestCreateOrder_ProductNotFound(t *testing.T) {
	uc, _ := buildUseCase(
		&stubCatalog{err: model.ErrProductNotFound},
		nil, nil,
		&stubPayment{},
	)
	_, err := uc.CreateOrder(context.Background(), application.CreateOrderRequest{
		MerchantID: "m1", UserID: "u1", ProductID: "xxx",
	}, application.PaymentDetail{CardToken: "ct_xxx"})
	if !errors.Is(err, model.ErrProductNotFound) {
		t.Errorf("want ErrProductNotFound, got %v", err)
	}
}

func TestCreateOrder_PaymentFails_CouponRolledBack(t *testing.T) {
	coupon := &stubCoupon{
		applied: &port.AppliedCoupon{CouponID: "cpn-1", DiscountType: "FIXED", DiscountValue: 500},
	}
	payment := &stubPayment{chargeErr: errors.New("declined")}
	uc, repo := buildUseCase(
		&stubCatalog{product: activeProduct()},
		coupon, nil,
		payment,
	)

	order, err := uc.CreateOrder(context.Background(), application.CreateOrderRequest{
		MerchantID: "m1", UserID: "u1", ProductID: "prod-1",
		CouponCode: "COUP",
	}, application.PaymentDetail{CardToken: "ct_xxx"})
	if !errors.Is(err, model.ErrPaymentFailed) {
		t.Errorf("want ErrPaymentFailed, got %v", err)
	}
	if coupon.rollback != 1 {
		t.Errorf("want 1 coupon rollback, got %d", coupon.rollback)
	}
	if order == nil {
		t.Fatal("expect failed order returned")
	}
	if order.Status != model.OrderStatusFailed {
		t.Errorf("want FAILED, got %s", order.Status)
	}
	// Failed order should be persisted
	saved, findErr := repo.FindByID(context.Background(), order.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if saved.Status != model.OrderStatusFailed {
		t.Errorf("want saved FAILED, got %s", saved.Status)
	}
}

func TestCreateOrder_MissingMerchantID(t *testing.T) {
	uc, _ := buildUseCase(
		&stubCatalog{product: activeProduct()},
		nil, nil, &stubPayment{},
	)
	_, err := uc.CreateOrder(context.Background(), application.CreateOrderRequest{
		UserID: "u1", ProductID: "prod-1",
	}, application.PaymentDetail{CardToken: "ct_xxx"})
	if !errors.Is(err, model.ErrMerchantRequired) {
		t.Errorf("want ErrMerchantRequired, got %v", err)
	}
}

func TestCaptureOrder_Success(t *testing.T) {
	payment := &stubPayment{
		chargeResult: &port.ChargeResult{TransactionID: "txn-1", Status: "AUTHORIZED"},
	}
	uc, _ := buildUseCase(
		&stubCatalog{product: activeProduct()},
		nil, nil, payment,
	)
	order, _ := uc.CreateOrder(context.Background(), application.CreateOrderRequest{
		MerchantID: "m1", UserID: "u1", ProductID: "prod-1",
	}, application.PaymentDetail{CardToken: "ct_xxx"})

	captured, err := uc.CaptureOrder(context.Background(), "u1", order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if captured.Status != model.OrderStatusPaid {
		t.Errorf("want PAID, got %s", captured.Status)
	}
}

func TestCaptureOrder_WrongUser(t *testing.T) {
	payment := &stubPayment{
		chargeResult: &port.ChargeResult{TransactionID: "txn-1", Status: "AUTHORIZED"},
	}
	uc, _ := buildUseCase(
		&stubCatalog{product: activeProduct()},
		nil, nil, payment,
	)
	order, _ := uc.CreateOrder(context.Background(), application.CreateOrderRequest{
		MerchantID: "m1", UserID: "u1", ProductID: "prod-1",
	}, application.PaymentDetail{CardToken: "ct_xxx"})

	_, err := uc.CaptureOrder(context.Background(), "other-user", order.ID)
	if !errors.Is(err, model.ErrOrderNotFound) {
		t.Errorf("want ErrOrderNotFound, got %v", err)
	}
}

func TestRefundOrder_Success(t *testing.T) {
	payment := &stubPayment{
		chargeResult: &port.ChargeResult{TransactionID: "txn-1", Status: "AUTHORIZED"},
	}
	uc, _ := buildUseCase(
		&stubCatalog{product: activeProduct()},
		nil, nil, payment,
	)
	order, _ := uc.CreateOrder(context.Background(), application.CreateOrderRequest{
		MerchantID: "m1", UserID: "u1", ProductID: "prod-1",
	}, application.PaymentDetail{CardToken: "ct_xxx"})
	_, _ = uc.CaptureOrder(context.Background(), "u1", order.ID)

	refunded, err := uc.RefundOrder(context.Background(), "u1", order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refunded.Status != model.OrderStatusRefunded {
		t.Errorf("want REFUNDED, got %s", refunded.Status)
	}
}

func TestGetOrder_Success(t *testing.T) {
	payment := &stubPayment{
		chargeResult: &port.ChargeResult{TransactionID: "txn-1", Status: "AUTHORIZED"},
	}
	uc, _ := buildUseCase(
		&stubCatalog{product: activeProduct()},
		nil, nil, payment,
	)
	order, _ := uc.CreateOrder(context.Background(), application.CreateOrderRequest{
		MerchantID: "m1", UserID: "u1", ProductID: "prod-1",
	}, application.PaymentDetail{CardToken: "ct_xxx"})

	got, err := uc.GetOrder(context.Background(), "u1", order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != order.ID {
		t.Errorf("want %s, got %s", order.ID, got.ID)
	}
}

func TestGetOrder_WrongUser(t *testing.T) {
	payment := &stubPayment{
		chargeResult: &port.ChargeResult{TransactionID: "txn-1", Status: "AUTHORIZED"},
	}
	uc, _ := buildUseCase(
		&stubCatalog{product: activeProduct()},
		nil, nil, payment,
	)
	order, _ := uc.CreateOrder(context.Background(), application.CreateOrderRequest{
		MerchantID: "m1", UserID: "u1", ProductID: "prod-1",
	}, application.PaymentDetail{CardToken: "ct_xxx"})

	_, err := uc.GetOrder(context.Background(), "other", order.ID)
	if !errors.Is(err, model.ErrOrderNotFound) {
		t.Errorf("want ErrOrderNotFound, got %v", err)
	}
}

func TestCreateOrder_PricingCalculation_NoCouponNoTax(t *testing.T) {
	payment := &stubPayment{
		chargeResult: &port.ChargeResult{TransactionID: "txn-1", Status: "AUTHORIZED"},
	}
	uc, _ := buildUseCase(
		&stubCatalog{product: activeProduct()},
		nil, nil, payment,
	)
	order, err := uc.CreateOrder(context.Background(), application.CreateOrderRequest{
		MerchantID: "m1", UserID: "u1", ProductID: "prod-1",
	}, application.PaymentDetail{CardToken: "ct_xxx"})
	if err != nil {
		t.Fatal(err)
	}
	if order.Price.FinalAmount != (money.NewMoney(10000, "USD")) {
		t.Errorf("want 10000 USD, got %v", order.Price.FinalAmount)
	}
}
