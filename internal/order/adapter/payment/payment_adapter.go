package payment

import (
	"context"

	acquiringApp "payment-demo/internal/acquiring/application"
	acquiringModel "payment-demo/internal/acquiring/domain/model"
	"payment-demo/internal/order/domain/port"
)

// PaymentCommandAdapter 实现 order.port.PaymentCommand，调用 payment.ChargeUseCase
type PaymentCommandAdapter struct {
	uc *acquiringApp.ChargeUseCase
}

var _ port.PaymentCommand = (*PaymentCommandAdapter)(nil)

func NewPaymentCommandAdapter(uc *acquiringApp.ChargeUseCase) *PaymentCommandAdapter {
	return &PaymentCommandAdapter{uc: uc}
}

func (a *PaymentCommandAdapter) Charge(ctx context.Context, req port.ChargeRequest) (*port.ChargeResult, error) {
	switch req.PaymentMethod {
	case "PAYPAL":
		txn, err := a.uc.PayPalPurchase(ctx, acquiringApp.PayPalPurchaseRequest{
			MerchantID: req.MerchantID,
			UserID:     req.UserID,
			OrderID:    req.OrderID,
			Amount:     req.Amount,
			Token:      acquiringModel.PayPalToken{OrderID: req.PayPalOrderID, PayerID: req.PayPalPayerID},
		})
		if err != nil {
			return nil, err
		}
		return &port.ChargeResult{
			TransactionID: string(txn.ID),
			Status:        string(txn.Status),
			ProviderRef:   txn.ProviderRef,
		}, nil
	default:
		txn, err := a.uc.Purchase(ctx, acquiringApp.PurchaseRequest{
			MerchantID:  req.MerchantID,
			UserID:      req.UserID,
			OrderID:     req.OrderID,
			Amount:      req.Amount,
			Token:       acquiringModel.CardToken{TokenID: req.CardToken, Last4: req.CardLast4, Brand: req.CardBrand},
			SavedCardID: req.SavedCardID,
			SaveCard:    req.SaveCard,
		})
		if err != nil {
			return nil, err
		}
		return &port.ChargeResult{
			TransactionID: string(txn.ID),
			Status:        string(txn.Status),
			ProviderRef:   txn.ProviderRef,
		}, nil
	}
}

func (a *PaymentCommandAdapter) Capture(ctx context.Context, userID, transactionID string) error {
	_, err := a.uc.Capture(ctx, userID, acquiringModel.TransactionID(transactionID))
	return err
}

func (a *PaymentCommandAdapter) Refund(ctx context.Context, userID, transactionID string) error {
	_, err := a.uc.Refund(ctx, userID, acquiringModel.TransactionID(transactionID))
	return err
}
