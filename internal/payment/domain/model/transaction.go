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

// PaymentTransaction 支付交易聚合根
// 状态机: Created → Authorized → Captured → Refunded
type PaymentTransaction struct {
	ID           TransactionID
	UserID       string
	ProductID    string // 来自 CatalogQuery 的商品 ID
	Amount       Money
	CardToken    CardToken
	Status       TransactionStatus
	ProviderRef  string // 外部支付商返回的引用 ID
	AuthCode     string
	FailReason   string
	AuthorizedAt *time.Time
	CapturedAt   *time.Time
	RefundedAt   *time.Time
	CreatedAt    time.Time
	Events       []event.DomainEvent // 未发布的领域事件
}

// NewPaymentTransaction 创建一笔新交易
func NewPaymentTransaction(userID, productID string, amount Money, token CardToken) *PaymentTransaction {
	return &PaymentTransaction{
		ID:        NewTransactionID(),
		UserID:    userID,
		ProductID: productID,
		Amount:    amount,
		CardToken: token,
		Status:    StatusCreated,
		CreatedAt: time.Now(),
	}
}

// MarkAuthorized 授权成功
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

// MarkCaptured 扣款成功
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

// MarkRefunded 退款成功
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

// MarkFailed 标记失败
func (t *PaymentTransaction) MarkFailed(reason string) {
	t.Status = StatusFailed
	t.FailReason = reason
}

func (t *PaymentTransaction) addEvent(e event.DomainEvent) {
	t.Events = append(t.Events, e)
}

// ClearEvents 发布后清空事件
func (t *PaymentTransaction) ClearEvents() []event.DomainEvent {
	events := t.Events
	t.Events = nil
	return events
}
