// Package coupon 提供 payment → coupon 的 ACL 适配器。
// 实现 payment/domain/port.CouponApplier，封装 coupon 聚合根的查询+应用+回滚。
package coupon

import (
	"context"
	"time"

	couponModel "payment-demo/internal/coupon/domain/model"
	couponPort "payment-demo/internal/coupon/domain/port"
	"payment-demo/internal/payment/domain/port"
)

// CouponAdapter ACL 适配器：payment → coupon。
type CouponAdapter struct {
	repo couponPort.CouponRepository
}

var _ port.CouponApplier = (*CouponAdapter)(nil)

// NewCouponAdapter 构造函数注入 coupon 的 CouponRepository。
func NewCouponAdapter(repo couponPort.CouponRepository) *CouponAdapter {
	return &CouponAdapter{repo: repo}
}

// Apply 校验可用性 → 应用优惠券 → 持久化 → 返回折扣信息。
func (a *CouponAdapter) Apply(ctx context.Context, couponCode string, userID string) (*port.AppliedCoupon, error) {
	c, err := a.repo.FindByCode(ctx, couponModel.CouponCode(couponCode))
	if err != nil {
		return nil, couponModel.ErrCouponNotFound
	}
	if !c.IsApplicable(time.Now()) {
		return nil, couponModel.ErrCouponNotApplicable
	}

	if err := c.Apply(userID, time.Now()); err != nil {
		return nil, err
	}
	if err := a.repo.Save(ctx, c); err != nil {
		return nil, err
	}

	return &port.AppliedCoupon{
		CouponID:      string(c.ID),
		DiscountType:  string(c.Rule.Type),
		DiscountValue: c.Rule.Value,
	}, nil
}

// Rollback 回滚优惠券使用（Saga 补偿）。
func (a *CouponAdapter) Rollback(ctx context.Context, couponCode string) error {
	c, err := a.repo.FindByCode(ctx, couponModel.CouponCode(couponCode))
	if err != nil {
		return err
	}
	c.Rollback()
	return a.repo.Save(ctx, c)
}
