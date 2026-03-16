package persistence

import (
	"context"
	"sync"

	"payment-demo/internal/catalog/domain/model"
	"payment-demo/internal/catalog/domain/port"
)

type InMemoryProductRepository struct {
	mu   sync.RWMutex
	data map[model.ProductID]*model.Product
}

var _ port.ProductRepository = (*InMemoryProductRepository)(nil)

func NewInMemoryProductRepository() *InMemoryProductRepository {
	return &InMemoryProductRepository{
		data: map[model.ProductID]*model.Product{
			"gem_100": {
				ID:     "gem_100",
				Name:   "100 Gems",
				Price:  model.Money{Amount: 999, Currency: "USD"},
				Status: model.ProductStatusActive,
			},
			"gem_500": {
				ID:     "gem_500",
				Name:   "500 Gems",
				Price:  model.Money{Amount: 3999, Currency: "USD"},
				Status: model.ProductStatusActive,
			},
			"gem_old": {
				ID:     "gem_old",
				Name:   "Legacy Pack",
				Price:  model.Money{Amount: 199, Currency: "USD"},
				Status: model.ProductStatusOffline,
			},
		},
	}
}

func (r *InMemoryProductRepository) FindByID(_ context.Context, id model.ProductID) (*model.Product, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.data[id]
	if !ok {
		return nil, model.ErrProductNotFound
	}
	return p, nil
}

func (r *InMemoryProductRepository) FindAll(_ context.Context) ([]*model.Product, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*model.Product, 0, len(r.data))
	for _, p := range r.data {
		result = append(result, p)
	}
	return result, nil
}
