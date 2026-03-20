// Package persistence 提供 acquiring 上下文的 InMemory 仓储实现。
package persistence

import (
	"context"
	"sync"

	"payment-demo/internal/acquiring/domain/model"
	"payment-demo/internal/acquiring/domain/port"
)

// InMemoryMerchantRepository 基于内存的商户仓储，线程安全。
// 适用于 Demo 环境；生产环境替换为 PostgreSQL 实现。
type InMemoryMerchantRepository struct {
	mu   sync.RWMutex
	data map[model.MerchantID]*model.Merchant
}

// 编译期接口检查：确保 InMemoryMerchantRepository 实现 port.MerchantRepository。
var _ port.MerchantRepository = (*InMemoryMerchantRepository)(nil)

// NewInMemoryMerchantRepository 构造函数，初始化内部 map。
func NewInMemoryMerchantRepository() *InMemoryMerchantRepository {
	return &InMemoryMerchantRepository{
		data: make(map[model.MerchantID]*model.Merchant),
	}
}

// Save 新增或覆盖商户聚合根（upsert 语义）。
func (r *InMemoryMerchantRepository) Save(_ context.Context, m *model.Merchant) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[m.ID] = m
	return nil
}

// FindByID 按商户 ID 查询，不存在时返回 model.ErrMerchantNotFound。
func (r *InMemoryMerchantRepository) FindByID(_ context.Context, id model.MerchantID) (*model.Merchant, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.data[id]
	if !ok {
		return nil, model.ErrMerchantNotFound
	}
	return m, nil
}

// FindAll 返回所有商户的快照列表（每次调用创建新切片，避免外部修改内部状态）。
func (r *InMemoryMerchantRepository) FindAll(_ context.Context) ([]*model.Merchant, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*model.Merchant, 0, len(r.data))
	for _, m := range r.data {
		result = append(result, m)
	}
	return result, nil
}
