package persistence

import (
	"context"
	"sync"

	"payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// InMemoryTransactionRepository 内存仓储实现
type InMemoryTransactionRepository struct {
	mu   sync.RWMutex
	data map[model.TransactionID]*model.PaymentTransaction
}

var _ port.TransactionRepository = (*InMemoryTransactionRepository)(nil)

func NewInMemoryTransactionRepository() *InMemoryTransactionRepository {
	return &InMemoryTransactionRepository{
		data: make(map[model.TransactionID]*model.PaymentTransaction),
	}
}

func (r *InMemoryTransactionRepository) Save(_ context.Context, txn *model.PaymentTransaction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[txn.ID] = txn
	return nil
}

func (r *InMemoryTransactionRepository) FindByID(_ context.Context, id model.TransactionID) (*model.PaymentTransaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	txn, ok := r.data[id]
	if !ok {
		return nil, model.ErrTransactionNotFound
	}
	return txn, nil
}
