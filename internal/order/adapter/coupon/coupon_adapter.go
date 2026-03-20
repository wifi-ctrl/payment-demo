package coupon

import (
	"context"
	"time"

	couponModel "payment-demo/internal/coupon/domain/model"
	couponPort "payment-demo/internal/coupon/domain/port"
	"payment-demo/internal/order/domain/port"
)

type CouponAdapter struct {
	repo couponPort.CouponRepository
}

var _ port.CouponApplier = (*CouponAdapter)(nil)

func NewCouponAdapter(repo couponPort.CouponRepository) *CouponAdapter {
	return &CouponAdapter{repo: repo}
}

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

func (a *CouponAdapter) Rollback(ctx context.Context, couponCode string) error {
	c, err := a.repo.FindByCode(ctx, couponModel.CouponCode(couponCode))
	if err != nil {
		return err
	}
	c.Rollback()
	return a.repo.Save(ctx, c)
}
