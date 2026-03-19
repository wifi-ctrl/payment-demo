// Package model 定义 coupon 上下文的聚合根、值对象和状态枚举。
package model

import (
	"time"

	"github.com/google/uuid"

	"payment-demo/internal/coupon/domain/event"
)

// CouponID 优惠券唯一标识
type CouponID string

// CouponCode 用户输入的业务编码，如 "SAVE10"
type CouponCode string

// DiscountType 折扣类型
type DiscountType string

const (
	DiscountTypePercentage DiscountType = "PERCENTAGE" // basis point，如 1000 = 10.00%
	DiscountTypeFixed      DiscountType = "FIXED"      // 固定金额（最小货币单位，如 cents）
)

// DiscountRule 折扣规则值对象
type DiscountRule struct {
	Type  DiscountType
	Value int64 // PERCENTAGE: basis point；FIXED: cents
}

// CouponStatus 优惠券状态枚举
type CouponStatus string

const (
	CouponStatusActive    CouponStatus = "ACTIVE"
	CouponStatusExhausted CouponStatus = "EXHAUSTED"
	CouponStatusExpired   CouponStatus = "EXPIRED"
)

// Coupon 优惠券聚合根。
// 状态机：ACTIVE → EXHAUSTED（用尽）| EXPIRED（过期）
type Coupon struct {
	ID         CouponID
	Code       CouponCode
	Rule       DiscountRule
	MaxUses    int // 0 = 不限
	UsedCount  int
	ValidFrom  time.Time
	ValidUntil time.Time
	Status     CouponStatus
	Events     []event.DomainEvent
}

// NewCoupon 工厂方法，创建一张 ACTIVE 状态的优惠券。
func NewCoupon(code CouponCode, rule DiscountRule, maxUses int, from, until time.Time) *Coupon {
	return &Coupon{
		ID:         CouponID(uuid.New().String()),
		Code:       code,
		Rule:       rule,
		MaxUses:    maxUses,
		UsedCount:  0,
		ValidFrom:  from,
		ValidUntil: until,
		Status:     CouponStatusActive,
	}
}

// IsApplicable 判断优惠券在给定时间是否可用：ACTIVE + 有效期内 + 有剩余次数。
func (c *Coupon) IsApplicable(now time.Time) bool {
	if c.Status != CouponStatusActive {
		return false
	}
	if now.Before(c.ValidFrom) || now.After(c.ValidUntil) {
		return false
	}
	if c.MaxUses > 0 && c.UsedCount >= c.MaxUses {
		return false
	}
	return true
}

// Apply 应用优惠券：校验可用性 → UsedCount++ → 若达上限则 EXHAUSTED → 发布事件。
// 由 pricing Saga 在计算定价时调用（通过 CouponRepository）。
func (c *Coupon) Apply(userID string, now time.Time) error {
	if !c.IsApplicable(now) {
		return ErrCouponNotApplicable
	}
	c.UsedCount++
	if c.MaxUses > 0 && c.UsedCount >= c.MaxUses {
		c.Status = CouponStatusExhausted
	}
	c.addEvent(event.CouponApplied{
		CouponID:   string(c.ID),
		UserID:     userID,
		OccurredAt: now,
	})
	return nil
}

// Rollback 回滚：UsedCount-- 并恢复 ACTIVE 状态。
// 补偿步骤：当 pricing Saga 后续步骤失败时调用，撤销 Apply 操作。
func (c *Coupon) Rollback() {
	if c.UsedCount > 0 {
		c.UsedCount--
	}
	if c.Status == CouponStatusExhausted {
		c.Status = CouponStatusActive
	}
}

// MarkExpired 标记优惠券为已过期（由定时任务或检查时调用）。
func (c *Coupon) MarkExpired() {
	c.Status = CouponStatusExpired
}

func (c *Coupon) addEvent(e event.DomainEvent) {
	c.Events = append(c.Events, e)
}

// ClearEvents 返回所有未发布的领域事件并清空，由 UseCase 调用后发布。
func (c *Coupon) ClearEvents() []event.DomainEvent {
	evts := c.Events
	c.Events = nil
	return evts
}
