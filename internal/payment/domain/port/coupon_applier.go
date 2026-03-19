package port

import "context"

// AppliedCoupon 优惠券应用结果（payment 视角，不暴露 coupon 内部模型）。
type AppliedCoupon struct {
	CouponID      string
	DiscountType  string // "PERCENTAGE" | "FIXED"
	DiscountValue int64  // PERCENTAGE: basis point；FIXED: cents
}

// CouponApplier payment 对 coupon 上下文的操作端口（消费方定义）。
// 封装查询+应用+回滚的完整生命周期，payment 不需要知道 coupon 的内部模型。
type CouponApplier interface {
	// Apply 校验可用性 → 应用优惠券 → 返回折扣信息。
	Apply(ctx context.Context, couponCode string, userID string) (*AppliedCoupon, error)

	// Rollback 回滚优惠券使用（Saga 补偿）。
	Rollback(ctx context.Context, couponCode string) error
}
