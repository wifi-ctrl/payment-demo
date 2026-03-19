// Package application 编排 coupon 上下文的用例。
package application

import (
	"context"
	"log"
	"time"

	"payment-demo/internal/coupon/domain/model"
	"payment-demo/internal/coupon/domain/port"
)

// CouponUseCase coupon 上下文用例编排层。
type CouponUseCase struct {
	repo port.CouponRepository
}

// NewCouponUseCase 构造函数注入仓储依赖。
func NewCouponUseCase(repo port.CouponRepository) *CouponUseCase {
	return &CouponUseCase{repo: repo}
}

// ─────────────────────────────────────────────────────────────────
// CreateCouponRequest / CreateCoupon
// ─────────────────────────────────────────────────────────────────

// CreateCouponRequest 创建优惠券请求。
type CreateCouponRequest struct {
	Code          string
	DiscountType  string // "PERCENTAGE" | "FIXED"
	DiscountValue int64  // PERCENTAGE: basis point；FIXED: cents
	MaxUses       int    // 0 = 不限
	ValidFrom     time.Time
	ValidUntil    time.Time
}

// CreateCoupon 创建新优惠券：校验编码唯一性 → 构造聚合根 → 持久化。
func (uc *CouponUseCase) CreateCoupon(ctx context.Context, req CreateCouponRequest) (*model.Coupon, error) {
	// 校验编码唯一性
	existing, err := uc.repo.FindByCode(ctx, model.CouponCode(req.Code))
	if err == nil && existing != nil {
		return nil, model.ErrCouponCodeConflict
	}

	// 校验折扣类型
	dt := model.DiscountType(req.DiscountType)
	if dt != model.DiscountTypePercentage && dt != model.DiscountTypeFixed {
		return nil, model.ErrCouponNotApplicable
	}

	rule := model.DiscountRule{
		Type:  dt,
		Value: req.DiscountValue,
	}

	coupon := model.NewCoupon(
		model.CouponCode(req.Code),
		rule,
		req.MaxUses,
		req.ValidFrom,
		req.ValidUntil,
	)

	if err := uc.repo.Save(ctx, coupon); err != nil {
		return nil, err
	}

	uc.publishEvents(coupon)
	return coupon, nil
}

// ─────────────────────────────────────────────────────────────────
// GetCouponByCode
// ─────────────────────────────────────────────────────────────────

// GetCouponByCode 按业务编码查询优惠券。
func (uc *CouponUseCase) GetCouponByCode(ctx context.Context, code string) (*model.Coupon, error) {
	return uc.repo.FindByCode(ctx, model.CouponCode(code))
}

// ─────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────

func (uc *CouponUseCase) publishEvents(c *model.Coupon) {
	for _, evt := range c.ClearEvents() {
		log.Printf("[DomainEvent] %s: %+v", evt.EventName(), evt)
	}
}
