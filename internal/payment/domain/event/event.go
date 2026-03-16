// Package event 定义 payment 上下文的领域事件。
// DomainEvent 接口统一由 internal/shared/event 提供，避免重复定义。
package event

import (
	"time"

	sharedEvent "payment-demo/internal/shared/event"
)

// DomainEvent 是 shared/event.DomainEvent 的本包别名，供上层无缝使用。
type DomainEvent = sharedEvent.DomainEvent

type PaymentAuthorized struct {
	TransactionID string
	Amount        int64
	Currency      string
	OccurredAt    time.Time
}

func (e PaymentAuthorized) EventName() string { return "payment.authorized" }

type PaymentCaptured struct {
	TransactionID string
	Amount        int64
	Currency      string
	OccurredAt    time.Time
}

func (e PaymentCaptured) EventName() string { return "payment.captured" }

type PaymentRefunded struct {
	TransactionID string
	Amount        int64
	Currency      string
	OccurredAt    time.Time
}

func (e PaymentRefunded) EventName() string { return "payment.refunded" }
