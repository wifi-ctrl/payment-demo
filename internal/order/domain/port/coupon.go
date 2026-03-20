package port

import "context"

type AppliedCoupon struct {
	CouponID      string
	DiscountType  string // "PERCENTAGE" | "FIXED"
	DiscountValue int64  // PERCENTAGE: basis point; FIXED: cents
}

type CouponApplier interface {
	Apply(ctx context.Context, couponCode string, userID string) (*AppliedCoupon, error)
	Rollback(ctx context.Context, couponCode string) error
}
