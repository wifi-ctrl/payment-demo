package persistence

import (
	"context"
	"sync"

	"payment-demo/internal/order/domain/model"
)

type InMemoryOrderRepository struct {
	mu     sync.Mutex
	orders map[model.OrderID]*model.Order
}

func NewInMemoryOrderRepository() *InMemoryOrderRepository {
	return &InMemoryOrderRepository{orders: make(map[model.OrderID]*model.Order)}
}

func (r *InMemoryOrderRepository) Save(_ context.Context, order *model.Order) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.orders[order.ID] = order
	return nil
}

func (r *InMemoryOrderRepository) FindByID(_ context.Context, id model.OrderID) (*model.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[id]
	if !ok {
		return nil, model.ErrOrderNotFound
	}
	return o, nil
}
