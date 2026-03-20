package port

import (
	"context"

	"payment-demo/internal/acquiring/domain/model"
)

// TransactionRepository 交易仓储端口（被驱动端口）
type TransactionRepository interface {
	Save(ctx context.Context, txn *model.PaymentTransaction) error
	FindByID(ctx context.Context, id model.TransactionID) (*model.PaymentTransaction, error)
	FindByProviderRef(ctx context.Context, providerRef string) (*model.PaymentTransaction, error)
}
