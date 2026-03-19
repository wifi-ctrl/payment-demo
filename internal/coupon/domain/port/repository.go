// Package port 定义 coupon 上下文的端口接口（消费方定义原则）。
package port

import (
	"context"

	"payment-demo/internal/coupon/domain/model"
)

// CouponRepository coupon 聚合根的仓储端口。
// InMemory 实现在 adapter/inmem/，生产实现在 adapter/persistence/（DB）。
type CouponRepository interface {
	// Save 新增或更新优惠券（upsert 语义）。
	Save(ctx context.Context, c *model.Coupon) error

	// FindByID 按主键查询，不存在时返回 model.ErrCouponNotFound。
	FindByID(ctx context.Context, id model.CouponID) (*model.Coupon, error)

	// FindByCode 按业务编码查询，不存在时返回 model.ErrCouponNotFound。
	FindByCode(ctx context.Context, code model.CouponCode) (*model.Coupon, error)
}
