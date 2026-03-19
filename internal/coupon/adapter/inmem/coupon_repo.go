// Package inmem 提供 coupon 上下文的内存仓储实现。
package inmem

import (
	"context"
	"sync"

	"payment-demo/internal/coupon/domain/model"
	"payment-demo/internal/coupon/domain/port"
)

// InMemoryCouponRepository 内存仓储，使用 sync.RWMutex 保护并发。
type InMemoryCouponRepository struct {
	mu       sync.RWMutex
	byID     map[model.CouponID]*model.Coupon
	byCode   map[model.CouponCode]*model.Coupon
}

// 编译期接口检查
var _ port.CouponRepository = (*InMemoryCouponRepository)(nil)

// NewInMemoryCouponRepository 构造函数。
func NewInMemoryCouponRepository() *InMemoryCouponRepository {
	return &InMemoryCouponRepository{
		byID:   make(map[model.CouponID]*model.Coupon),
		byCode: make(map[model.CouponCode]*model.Coupon),
	}
}

// Save 新增或更新优惠券（upsert 语义）。
func (r *InMemoryCouponRepository) Save(_ context.Context, c *model.Coupon) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[c.ID] = c
	r.byCode[c.Code] = c
	return nil
}

// FindByID 按主键查询，不存在时返回 model.ErrCouponNotFound。
func (r *InMemoryCouponRepository) FindByID(_ context.Context, id model.CouponID) (*model.Coupon, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.byID[id]
	if !ok {
		return nil, model.ErrCouponNotFound
	}
	return c, nil
}

// FindByCode 按业务编码查询，不存在时返回 model.ErrCouponNotFound。
func (r *InMemoryCouponRepository) FindByCode(_ context.Context, code model.CouponCode) (*model.Coupon, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.byCode[code]
	if !ok {
		return nil, model.ErrCouponNotFound
	}
	return c, nil
}
