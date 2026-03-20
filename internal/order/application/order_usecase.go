package application

import (
	"context"
	"log"

	"payment-demo/internal/order/domain/model"
	"payment-demo/internal/order/domain/port"
	"payment-demo/internal/order/domain/service"
)

// OrderUseCase 订单用例编排层。
//
// 职责：查商品 → 应用优惠券 → 算税 → 创建 Order → 调 Payment.Charge → 标记授权状态。
// Payment 只按 Order.FinalAmount 扣款，定价逻辑完全由 Order 掌控。
type OrderUseCase struct {
	repo          port.OrderRepository
	catalog       port.CatalogQuery
	couponApplier port.CouponApplier
	taxQuery      port.TaxRateQuery
	payment       port.PaymentCommand
}

func NewOrderUseCase(
	repo port.OrderRepository,
	catalog port.CatalogQuery,
	couponApplier port.CouponApplier,
	taxQuery port.TaxRateQuery,
	payment port.PaymentCommand,
) *OrderUseCase {
	return &OrderUseCase{
		repo:          repo,
		catalog:       catalog,
		couponApplier: couponApplier,
		taxQuery:      taxQuery,
		payment:       payment,
	}
}

// CreateOrderRequest 创建订单入参（仅 order 关心的字段）
type CreateOrderRequest struct {
	MerchantID string
	UserID     string
	ProductID  string
	CouponCode string
}

// PaymentDetail 支付明细，由 handler 层组装，order 只透传给 PaymentCommand。
type PaymentDetail struct {
	CardToken     string
	CardLast4     string
	CardBrand     string
	SavedCardID   string
	SaveCard      bool
	PaymentMethod string // "CARD" | "PAYPAL"
	PayPalOrderID string
	PayPalPayerID string
}

// CreateOrder 创建订单：定价 → 创建聚合 → 发起支付授权 → 返回 AUTHORIZED 订单。
func (uc *OrderUseCase) CreateOrder(ctx context.Context, req CreateOrderRequest, pay PaymentDetail) (*model.Order, error) {
	if req.MerchantID == "" {
		return nil, model.ErrMerchantRequired
	}

	product, err := uc.catalog.FindProduct(ctx, req.ProductID)
	if err != nil {
		return nil, err
	}
	if !product.IsActive {
		return nil, model.ErrProductNotActive
	}

	original := model.NewMoney(product.Amount, product.Currency)
	finalAmount, discountAmount, taxAmount, coupon, err := uc.resolvePricing(ctx, req.UserID, req.ProductID, original, req.CouponCode)
	if err != nil {
		return nil, err
	}

	var couponID string
	if coupon != nil {
		couponID = coupon.CouponID
	}

	order := model.NewOrder(req.UserID, req.MerchantID, req.ProductID, product.Name,
		model.PriceBreakdown{
			OriginalAmount: original,
			DiscountAmount: discountAmount,
			TaxAmount:      taxAmount,
			FinalAmount:    finalAmount,
		}, couponID)

	log.Printf("[OrderUseCase] CreateOrder: order=%s, user=%s, product=%s, final=%d %s",
		order.ID, req.UserID, product.Name, finalAmount.Amount, finalAmount.Currency)

	chargeResult, err := uc.payment.Charge(ctx, port.ChargeRequest{
		MerchantID:    req.MerchantID,
		UserID:        req.UserID,
		OrderID:       string(order.ID),
		Amount:        finalAmount,
		CardToken:     pay.CardToken,
		CardLast4:     pay.CardLast4,
		CardBrand:     pay.CardBrand,
		SavedCardID:   pay.SavedCardID,
		SaveCard:      pay.SaveCard,
		PaymentMethod: pay.PaymentMethod,
		PayPalOrderID: pay.PayPalOrderID,
		PayPalPayerID: pay.PayPalPayerID,
	})
	if err != nil {
		uc.rollbackCoupon(ctx, req.CouponCode)
		_ = order.MarkFailed()
		if saveErr := uc.repo.Save(ctx, order); saveErr != nil {
			log.Printf("[OrderUseCase] failed to save failed order: %v", saveErr)
		}
		return order, model.ErrPaymentFailed
	}

	if err := order.MarkAuthorized(chargeResult.TransactionID); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, order); err != nil {
		uc.rollbackCoupon(ctx, req.CouponCode)
		return nil, err
	}
	uc.publishEvents(order)
	return order, nil
}

// CaptureOrder 扣款：AUTHORIZED → PAID
func (uc *OrderUseCase) CaptureOrder(ctx context.Context, userID string, orderID model.OrderID) (*model.Order, error) {
	order, err := uc.repo.FindByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.UserID != userID {
		return nil, model.ErrOrderNotFound
	}

	if err := uc.payment.Capture(ctx, userID, order.TransactionID); err != nil {
		return nil, err
	}
	if err := order.MarkPaid(); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, order); err != nil {
		return nil, err
	}
	uc.publishEvents(order)
	return order, nil
}

// RefundOrder 退款：PAID → REFUNDED
func (uc *OrderUseCase) RefundOrder(ctx context.Context, userID string, orderID model.OrderID) (*model.Order, error) {
	order, err := uc.repo.FindByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.UserID != userID {
		return nil, model.ErrOrderNotFound
	}

	if err := uc.payment.Refund(ctx, userID, order.TransactionID); err != nil {
		return nil, err
	}
	if err := order.MarkRefunded(); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, order); err != nil {
		return nil, err
	}
	uc.publishEvents(order)
	return order, nil
}

// GetOrder 查询订单
func (uc *OrderUseCase) GetOrder(ctx context.Context, userID string, orderID model.OrderID) (*model.Order, error) {
	order, err := uc.repo.FindByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.UserID != userID {
		return nil, model.ErrOrderNotFound
	}
	return order, nil
}

func (uc *OrderUseCase) resolvePricing(
	ctx context.Context, userID, productID string, original model.Money, couponCode string,
) (finalAmount, discountAmount, taxAmount model.Money, coupon *port.AppliedCoupon, err error) {
	var discountType string
	var discountValue int64
	if couponCode != "" && uc.couponApplier != nil {
		coupon, err = uc.couponApplier.Apply(ctx, couponCode, userID)
		if err != nil {
			return model.Money{}, model.Money{}, model.Money{}, nil, err
		}
		discountType = coupon.DiscountType
		discountValue = coupon.DiscountValue
	}
	taxBP := uc.queryTaxRate(ctx, productID, original.Currency)
	finalAmount, discountAmount, taxAmount, err = service.CalculateFinalAmount(original, discountType, discountValue, taxBP)
	if err != nil {
		uc.rollbackCoupon(ctx, couponCode)
		return model.Money{}, model.Money{}, model.Money{}, coupon, err
	}
	return finalAmount, discountAmount, taxAmount, coupon, nil
}

func (uc *OrderUseCase) queryTaxRate(ctx context.Context, productID, currency string) int64 {
	if uc.taxQuery == nil {
		return 0
	}
	bp, err := uc.taxQuery.FindTaxRate(ctx, productID, currency)
	if err != nil {
		log.Printf("[OrderUseCase] TaxRateQuery failed (productID=%s, currency=%s): %v — using 0", productID, currency, err)
		return 0
	}
	return bp
}

func (uc *OrderUseCase) rollbackCoupon(ctx context.Context, couponCode string) {
	if couponCode == "" || uc.couponApplier == nil {
		return
	}
	if err := uc.couponApplier.Rollback(ctx, couponCode); err != nil {
		log.Printf("[OrderUseCase] coupon rollback failed: %v", err)
	}
}

func (uc *OrderUseCase) publishEvents(order *model.Order) {
	for _, evt := range order.ClearEvents() {
		log.Printf("[DomainEvent] %s: %v", evt.EventName(), evt)
	}
}
