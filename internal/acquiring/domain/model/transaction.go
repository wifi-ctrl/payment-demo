package model

import (
	"time"

	"github.com/google/uuid"

	"payment-demo/internal/acquiring/domain/event"
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
//
// Channel / RecurringToken / SaveCard / SavedCardID 在 Authorize 阶段写入，
// Capture 成功后才触发绑卡 / 存储 ChannelToken — 确保只持久化经过网关验证+扣款成功的卡。
//
// 定价相关字段（ProductID, DiscountAmount, TaxAmount, CouponID）已迁移至 Order 聚合根。
// Payment 只负责按 Order 给定的 FinalAmount 扣款。
type PaymentTransaction struct {
	ID          TransactionID
	MerchantID  string        // 归属商户
	UserID      string
	OrderID     string        // 关联的 Order ID（定价明细由 Order 管理）
	Amount      Money
	Method      PaymentMethod // CARD / PAYPAL
	CardToken   CardToken     // Method==CARD 时有效
	PayPalToken PayPalToken   // Method==PAYPAL 时有效
	Status         TransactionStatus
	ProviderRef    string // 外部支付商返回的引用 ID
	AuthCode       string
	FailReason     string
	Channel        string // 实际扣款渠道 ("stripe", "adyen"...)
	RecurringToken string // 渠道返回的复购 token（Authorize 时获得，Capture 后持久化）
	SaveCard       bool   // 首购时用户是否要求绑卡（Capture 后决定是否执行）
	SavedCardID    string // 复购路径：已存卡 ID（Capture 后回存 ChannelToken）
	AuthorizedAt *time.Time
	CapturedAt   *time.Time
	RefundedAt   *time.Time
	CreatedAt    time.Time
	Events       []event.DomainEvent
}

// NewPaymentTransaction 创建一笔新的卡支付交易（Card 流程）。
func NewPaymentTransaction(userID, orderID string, amount Money, token CardToken) *PaymentTransaction {
	return &PaymentTransaction{
		ID:        NewTransactionID(),
		UserID:    userID,
		OrderID:   orderID,
		Amount:    amount,
		Method:    PaymentMethodCard,
		CardToken: token,
		Status:    StatusCreated,
		CreatedAt: time.Now(),
	}
}

// NewPayPalTransaction 创建一笔新的 PayPal 支付交易（PayPal 流程）。
func NewPayPalTransaction(userID, orderID string, amount Money, token PayPalToken) *PaymentTransaction {
	return &PaymentTransaction{
		ID:          NewTransactionID(),
		UserID:      userID,
		OrderID:     orderID,
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

// RecordAuthContext 记录授权阶段的渠道上下文（Capture 后用于绑卡/存 ChannelToken）
func (t *PaymentTransaction) RecordAuthContext(channel, recurringToken, savedCardID string, saveCard bool) {
	t.Channel = channel
	t.RecurringToken = recurringToken
	t.SavedCardID = savedCardID
	t.SaveCard = saveCard
}

// RecordRecurringToken 渠道异步回调时补充 recurring token
func (t *PaymentTransaction) RecordRecurringToken(channel, token string) {
	t.Channel = channel
	t.RecurringToken = token
}

// MarkFailed 标记失败（任意状态均可流转，仅记录失败原因）
func (t *PaymentTransaction) MarkFailed(reason string) {
	t.Status = StatusFailed
	t.FailReason = reason
}

func (t *PaymentTransaction) addEvent(e event.DomainEvent) {
	t.Events = append(t.Events, e)
}

// ValidateCapturable 校验交易是否可扣款（须为 AUTHORIZED 状态）。
func (t *PaymentTransaction) ValidateCapturable() error {
	if t.Status != StatusAuthorized {
		return ErrInvalidStateTransition
	}
	return nil
}

// ValidateRefundable 校验交易是否可退款（须为 CAPTURED 状态）。
func (t *PaymentTransaction) ValidateRefundable() error {
	if t.Status != StatusCaptured {
		return ErrInvalidStateTransition
	}
	return nil
}

// ClearEvents 返回所有未发布的领域事件并清空，由 UseCase 调用后发布
func (t *PaymentTransaction) ClearEvents() []event.DomainEvent {
	events := t.Events
	t.Events = nil
	return events
}
