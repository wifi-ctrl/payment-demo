package port

import (
	"context"

	"payment-demo/internal/order/domain/model"
)

type OrderRepository interface {
	Save(ctx context.Context, order *model.Order) error
	FindByID(ctx context.Context, id model.OrderID) (*model.Order, error)
}
