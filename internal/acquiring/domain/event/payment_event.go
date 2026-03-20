package event

import "time"

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
