package port

import (
	"context"

	"payment-demo/internal/shared/money"
)

type ChargeRequest struct {
	MerchantID  string
	UserID      string
	OrderID     string
	Amount      money.Money
	CardToken   string
	CardLast4   string
	CardBrand   string
	SavedCardID string
	SaveCard    bool
	PaymentMethod string // "CARD" | "PAYPAL"
	PayPalOrderID string
	PayPalPayerID string
}

type ChargeResult struct {
	TransactionID string
	Status        string
	ProviderRef   string
}

// PaymentCommand order 上下文对 acquiring 上下文的操作端口
type PaymentCommand interface {
	Charge(ctx context.Context, req ChargeRequest) (*ChargeResult, error)
	Capture(ctx context.Context, userID, transactionID string) error
	Refund(ctx context.Context, userID, transactionID string) error
}
