// Package event 定义 coupon 上下文的领域事件。
// DomainEvent 接口统一由 internal/shared/event 提供，避免重复定义。
package event

import (
	"time"

	sharedEvent "payment-demo/internal/shared/event"
)

// DomainEvent 是 shared/event.DomainEvent 的本包别名，供上层无缝使用。
type DomainEvent = sharedEvent.DomainEvent

// CouponApplied 优惠券被成功应用时触发。
type CouponApplied struct {
	CouponID   string
	UserID     string
	OccurredAt time.Time
}

func (e CouponApplied) EventName() string { return "coupon.applied" }
