package event

import (
	"time"

	sharedEvent "payment-demo/internal/shared/event"
)

type DomainEvent = sharedEvent.DomainEvent

type OrderCreated struct {
	OrderID    string
	UserID     string
	Amount     int64
	Currency   string
	OccurredAt time.Time
}

func (e OrderCreated) EventName() string { return "order.created" }

type OrderPaid struct {
	OrderID       string
	TransactionID string
	Amount        int64
	Currency      string
	OccurredAt    time.Time
}

func (e OrderPaid) EventName() string { return "order.paid" }

type OrderRefunded struct {
	OrderID       string
	TransactionID string
	OccurredAt    time.Time
}

func (e OrderRefunded) EventName() string { return "order.refunded" }
