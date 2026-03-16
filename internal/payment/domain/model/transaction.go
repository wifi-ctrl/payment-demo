package model

import (
	"time"

	"github.com/google/uuid"

	"payment-demo/internal/payment/domain/event"
)

// TransactionID 交易唯一标识
type TransactionID string

func NewTransactionID() TransactionID {
	return TransactionID(uuid.New().String())
}

// TransactionStatus 交易状态枚举
type TransactionStatus string

const (
	StatusCreated    TransactionStatus = "CREATED"
	StatusAuthorized TransactionStatus = "AUTHORIZED"
	StatusCaptured   TransactionStatus = "CAPTURED"
	StatusRefunded   TransactionStatus = "REFUNDED"
	StatusFailed     TransactionStatus = "FAILED"
)

// PaymentTransaction 支付交易聚合根。
// 状态机: Created → Authorized → Captured → Refunded
// Method 字段区分 CARD / PAYPAL，决定 UseCase 路由到哪个 Gateway。
// MerchantID 冗余存储，Capture/Refund 时用于重建商户专属 Gateway（多商户路由）。
type PaymentTransaction struct {
	ID          TransactionID
	MerchantID  string    // 归属商户，由 Purchase/PayPalPurchase 写入，Capture/Refund 时复用
	UserID      string
	ProductID   string // 来自 CatalogQuery 的商品 ID
	Amount      Money
	Method      PaymentMethod // CARD / PAYPAL，默认 CARD
	CardToken   CardToken     // Method==CARD 时有效
	PayPalToken PayPalToken   // Method==PAYPAL 时有效
	Status      TransactionStatus
	ProviderRef string // 外部支付商返回的引用 ID
	AuthCode    string
	FailReason  string
	AuthorizedAt *time.Time
	CapturedAt   *time.Time
	RefundedAt   *time.Time
	CreatedAt    time.Time
	Events       []event.DomainEvent // 未发布的领域事件
}

// NewPaymentTransaction 创建一笔新的卡支付交易（Card 流程）。
func NewPaymentTransaction(userID, productID string, amount Money, token CardToken) *PaymentTransaction {
	return &PaymentTransaction{
		ID:        NewTransactionID(),
		UserID:    userID,
		ProductID: productID,
		Amount:    amount,
		Method:    PaymentMethodCard,
		CardToken: token,
		Status:    StatusCreated,
		CreatedAt: time.Now(),
	}
}

// NewPayPalTransaction 创建一笔新的 PayPal 支付交易（PayPal 流程）。
// 对应 Card 流程的 NewPaymentTransaction，Method 固定为 PaymentMethodPayPal。
func NewPayPalTransaction(userID, productID string, amount Money, token PayPalToken) *PaymentTransaction {
	return &PaymentTransaction{
		ID:          NewTransactionID(),
		UserID:      userID,
		ProductID:   productID,
		Amount:      amount,
		Method:      PaymentMethodPayPal,
		PayPalToken: token,
		Status:      StatusCreated,
		CreatedAt:   time.Now(),
	}
}

// MarkAuthorized 授权成功：StatusCreated → StatusAuthorized
func (t *PaymentTransaction) MarkAuthorized(providerRef, authCode string) error {
	if t.Status != StatusCreated {
		return ErrInvalidStateTransition
	}
	now := time.Now()
	t.Status = StatusAuthorized
	t.ProviderRef = providerRef
	t.AuthCode = authCode
	t.AuthorizedAt = &now
	t.addEvent(event.PaymentAuthorized{
		TransactionID: string(t.ID),
		Amount:        t.Amount.Amount,
		Currency:      t.Amount.Currency,
		OccurredAt:    now,
	})
	return nil
}

// MarkCaptured 扣款成功：StatusAuthorized → StatusCaptured
func (t *PaymentTransaction) MarkCaptured() error {
	if t.Status != StatusAuthorized {
		return ErrInvalidStateTransition
	}
	now := time.Now()
	t.Status = StatusCaptured
	t.CapturedAt = &now
	t.addEvent(event.PaymentCaptured{
		TransactionID: string(t.ID),
		Amount:        t.Amount.Amount,
		Currency:      t.Amount.Currency,
		OccurredAt:    now,
	})
	return nil
}

// MarkRefunded 退款成功：StatusCaptured → StatusRefunded
func (t *PaymentTransaction) MarkRefunded() error {
	if t.Status != StatusCaptured {
		return ErrInvalidStateTransition
	}
	now := time.Now()
	t.Status = StatusRefunded
	t.RefundedAt = &now
	t.addEvent(event.PaymentRefunded{
		TransactionID: string(t.ID),
		Amount:        t.Amount.Amount,
		Currency:      t.Amount.Currency,
		OccurredAt:    now,
	})
	return nil
}

// MarkFailed 标记失败（任意状态均可流转，仅记录失败原因）
func (t *PaymentTransaction) MarkFailed(reason string) {
	t.Status = StatusFailed
	t.FailReason = reason
}

func (t *PaymentTransaction) addEvent(e event.DomainEvent) {
	t.Events = append(t.Events, e)
}

// ClearEvents 返回所有未发布的领域事件并清空，由 UseCase 调用后发布
func (t *PaymentTransaction) ClearEvents() []event.DomainEvent {
	events := t.Events
	t.Events = nil
	return events
}
