package model

import (
	"time"

	"github.com/google/uuid"

	"payment-demo/internal/order/domain/event"
)

type OrderID string

func NewOrderID() OrderID { return OrderID(uuid.New().String()) }

type OrderStatus string

const (
	OrderStatusPendingPayment OrderStatus = "PENDING_PAYMENT"
	OrderStatusAuthorized     OrderStatus = "AUTHORIZED"
	OrderStatusPaid           OrderStatus = "PAID"
	OrderStatusRefunded       OrderStatus = "REFUNDED"
	OrderStatusFailed         OrderStatus = "FAILED"
)

// PriceBreakdown 定价明细值对象
type PriceBreakdown struct {
	OriginalAmount Money
	DiscountAmount Money
	TaxAmount      Money
	FinalAmount    Money
}

// Order 订单聚合根。
// 负责锁定商品价格 + 优惠券 + 税 → FinalAmount，并关联 Payment 交易。
// Payment 只按 FinalAmount 扣款，不关心定价细节。
type Order struct {
	ID            OrderID
	UserID        string
	MerchantID    string
	ProductID     string
	ProductName   string
	Status        OrderStatus
	Price         PriceBreakdown
	CouponID      string
	TransactionID string // 关联的 Payment 交易 ID
	CreatedAt     time.Time
	PaidAt        *time.Time
	Events        []event.DomainEvent
}

func NewOrder(userID, merchantID, productID, productName string, price PriceBreakdown, couponID string) *Order {
	o := &Order{
		ID:          NewOrderID(),
		UserID:      userID,
		MerchantID:  merchantID,
		ProductID:   productID,
		ProductName: productName,
		Status:      OrderStatusPendingPayment,
		Price:       price,
		CouponID:    couponID,
		CreatedAt:   time.Now(),
	}
	o.addEvent(event.OrderCreated{
		OrderID:    string(o.ID),
		UserID:     userID,
		Amount:     price.FinalAmount.Amount,
		Currency:   price.FinalAmount.Currency,
		OccurredAt: o.CreatedAt,
	})
	return o
}

// MarkAuthorized 支付授权成功：PENDING_PAYMENT → AUTHORIZED
func (o *Order) MarkAuthorized(transactionID string) error {
	if o.Status != OrderStatusPendingPayment {
		return ErrInvalidStateTransition
	}
	o.Status = OrderStatusAuthorized
	o.TransactionID = transactionID
	return nil
}

// MarkPaid 扣款成功：AUTHORIZED → PAID
func (o *Order) MarkPaid() error {
	if o.Status != OrderStatusAuthorized {
		return ErrInvalidStateTransition
	}
	now := time.Now()
	o.Status = OrderStatusPaid
	o.PaidAt = &now
	o.addEvent(event.OrderPaid{
		OrderID:       string(o.ID),
		TransactionID: o.TransactionID,
		Amount:        o.Price.FinalAmount.Amount,
		Currency:      o.Price.FinalAmount.Currency,
		OccurredAt:    now,
	})
	return nil
}

// MarkRefunded 退款成功：PAID → REFUNDED
func (o *Order) MarkRefunded() error {
	if o.Status != OrderStatusPaid {
		return ErrInvalidStateTransition
	}
	o.Status = OrderStatusRefunded
	o.addEvent(event.OrderRefunded{
		OrderID:       string(o.ID),
		TransactionID: o.TransactionID,
		OccurredAt:    time.Now(),
	})
	return nil
}

// MarkFailed 标记失败：PENDING_PAYMENT → FAILED（支付授权被拒）
func (o *Order) MarkFailed() error {
	if o.Status != OrderStatusPendingPayment {
		return ErrInvalidStateTransition
	}
	o.Status = OrderStatusFailed
	return nil
}

func (o *Order) addEvent(e event.DomainEvent) {
	o.Events = append(o.Events, e)
}

func (o *Order) ClearEvents() []event.DomainEvent {
	events := o.Events
	o.Events = nil
	return events
}
