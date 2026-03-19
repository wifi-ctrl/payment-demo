package model_test

import (
	"testing"
	"time"

	"payment-demo/internal/coupon/domain/event"
	"payment-demo/internal/coupon/domain/model"
)

// ─────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────

func now() time.Time              { return time.Now() }
func future() time.Time           { return time.Now().Add(7 * 24 * time.Hour) }
func past() time.Time             { return time.Now().Add(-24 * time.Hour) }
func validRule() model.DiscountRule {
	return model.DiscountRule{Type: model.DiscountTypePercentage, Value: 1000}
}

// ─────────────────────────────────────────────────────────────────
// AC-4: NewCoupon 工厂方法创建合法优惠券
// ─────────────────────────────────────────────────────────────────

func TestNewCoupon_Factory_CreatesActive(t *testing.T) {
	// AC-4
	from := now()
	until := future()
	rule := validRule()

	c := model.NewCoupon(model.CouponCode("SAVE10"), rule, 100, from, until)

	if c == nil {
		t.Fatal("expected non-nil coupon")
	}
	if string(c.ID) == "" {
		t.Error("expected non-empty ID")
	}
	if c.Status != model.CouponStatusActive {
		t.Errorf("expected Status=ACTIVE, got %s", c.Status)
	}
	if c.UsedCount != 0 {
		t.Errorf("expected UsedCount=0, got %d", c.UsedCount)
	}
	if len(c.Events) != 0 {
		t.Errorf("expected no events from factory, got %d", len(c.Events))
	}
	if c.Code != "SAVE10" {
		t.Errorf("expected Code=SAVE10, got %s", c.Code)
	}
	if c.Rule.Type != model.DiscountTypePercentage {
		t.Errorf("expected Rule.Type=PERCENTAGE, got %s", c.Rule.Type)
	}
	if c.Rule.Value != 1000 {
		t.Errorf("expected Rule.Value=1000, got %d", c.Rule.Value)
	}
	if c.MaxUses != 100 {
		t.Errorf("expected MaxUses=100, got %d", c.MaxUses)
	}
	if !c.ValidFrom.Equal(from) {
		t.Errorf("expected ValidFrom=%v, got %v", from, c.ValidFrom)
	}
	if !c.ValidUntil.Equal(until) {
		t.Errorf("expected ValidUntil=%v, got %v", until, c.ValidUntil)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-5: Coupon.Apply() 正常核销
// ─────────────────────────────────────────────────────────────────

func TestCoupon_Apply_Success(t *testing.T) {
	// AC-5
	c := model.NewCoupon("TEST", validRule(), 5, past(), future())

	err := c.Apply("user-1", now())

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if c.UsedCount != 1 {
		t.Errorf("expected UsedCount=1, got %d", c.UsedCount)
	}
	if c.Status != model.CouponStatusActive {
		t.Errorf("expected Status=ACTIVE, got %s", c.Status)
	}
	if len(c.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(c.Events))
	}
	evt, ok := c.Events[0].(event.CouponApplied)
	if !ok {
		t.Fatalf("expected CouponApplied event, got %T", c.Events[0])
	}
	if evt.EventName() != "coupon.applied" {
		t.Errorf("expected event name=coupon.applied, got %s", evt.EventName())
	}
	if string(c.ID) != evt.CouponID {
		t.Errorf("expected CouponID=%s in event, got %s", c.ID, evt.CouponID)
	}
	if evt.OccurredAt.IsZero() {
		t.Error("expected OccurredAt to be set")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-6: Coupon.Apply() 达到最大使用次数后状态转为 EXHAUSTED
// ─────────────────────────────────────────────────────────────────

func TestCoupon_Apply_ExhaustsOnMaxUses(t *testing.T) {
	// AC-6
	c := model.NewCoupon("ONCE", validRule(), 1, past(), future())

	err := c.Apply("user-1", now())

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if c.UsedCount != 1 {
		t.Errorf("expected UsedCount=1, got %d", c.UsedCount)
	}
	if c.Status != model.CouponStatusExhausted {
		t.Errorf("expected Status=EXHAUSTED, got %s", c.Status)
	}
	if len(c.Events) != 1 {
		t.Errorf("expected 1 CouponApplied event, got %d", len(c.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-7: Coupon.Apply() 在 EXHAUSTED 状态下拒绝
// ─────────────────────────────────────────────────────────────────

func TestCoupon_Apply_RefusedWhenExhausted(t *testing.T) {
	// AC-7
	c := model.NewCoupon("ONCE", validRule(), 1, past(), future())
	_ = c.Apply("user-1", now()) // exhaust it
	c.ClearEvents()              // clear first event

	err := c.Apply("user-2", now())

	if err != model.ErrCouponNotApplicable {
		t.Errorf("expected ErrCouponNotApplicable, got %v", err)
	}
	if c.UsedCount != 1 {
		t.Errorf("expected UsedCount unchanged=1, got %d", c.UsedCount)
	}
	if c.Status != model.CouponStatusExhausted {
		t.Errorf("expected Status=EXHAUSTED unchanged, got %s", c.Status)
	}
	if len(c.Events) != 0 {
		t.Errorf("expected no new events, got %d", len(c.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-8: Coupon.Apply() 已过有效期拒绝
// ─────────────────────────────────────────────────────────────────

func TestCoupon_Apply_RefusedWhenExpired(t *testing.T) {
	// AC-8
	// ValidUntil 在过去
	c := model.NewCoupon("OLD", validRule(), 5, past().Add(-48*time.Hour), past())

	err := c.Apply("user-1", now())

	if err != model.ErrCouponNotApplicable {
		t.Errorf("expected ErrCouponNotApplicable, got %v", err)
	}
	if c.Status != model.CouponStatusActive {
		t.Errorf("expected Status=ACTIVE unchanged, got %s", c.Status)
	}
	if len(c.Events) != 0 {
		t.Errorf("expected no events, got %d", len(c.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-9: Coupon.IsApplicable() 边界：ValidFrom 恰好等于 now
// ─────────────────────────────────────────────────────────────────

func TestCoupon_IsApplicable_ValidFromEqualsNow(t *testing.T) {
	// AC-9
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)
	c := model.NewCoupon("EDGE", model.DiscountRule{Type: model.DiscountTypeFixed, Value: 100}, 0, t0, t1)

	if !c.IsApplicable(t0) {
		t.Error("expected IsApplicable=true when now==ValidFrom")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-10: Coupon MaxUses=0 时不限使用次数
// ─────────────────────────────────────────────────────────────────

func TestCoupon_Apply_UnlimitedWhenMaxUsesZero(t *testing.T) {
	// AC-10
	c := model.NewCoupon("UNLIMITED", validRule(), 0, past(), future())
	c.UsedCount = 99 // 模拟已使用 99 次

	err := c.Apply("user-1", now())

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if c.UsedCount != 100 {
		t.Errorf("expected UsedCount=100, got %d", c.UsedCount)
	}
	if c.Status != model.CouponStatusActive {
		t.Errorf("expected Status=ACTIVE (unlimited), got %s", c.Status)
	}
}

// ─────────────────────────────────────────────────────────────────
// ClearEvents: 返回并清空，二次调用返回空
// ─────────────────────────────────────────────────────────────────

func TestCoupon_ClearEvents_ClearsAfterReturn(t *testing.T) {
	c := model.NewCoupon("CLR", validRule(), 5, past(), future())
	_ = c.Apply("user-1", now())

	if len(c.Events) == 0 {
		t.Fatal("expected events before ClearEvents")
	}

	evts := c.ClearEvents()
	if len(evts) != 1 {
		t.Errorf("expected 1 event returned, got %d", len(evts))
	}
	// 二次调用应返回空
	evts2 := c.ClearEvents()
	if len(evts2) != 0 {
		t.Errorf("expected 0 events on second ClearEvents, got %d", len(evts2))
	}
	if len(c.Events) != 0 {
		t.Errorf("expected c.Events to be empty, got %d", len(c.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// Rollback: UsedCount-- 并恢复 ACTIVE
// ─────────────────────────────────────────────────────────────────

func TestCoupon_Rollback_RestoresActiveAndCount(t *testing.T) {
	c := model.NewCoupon("ROLLBACK", validRule(), 1, past(), future())
	_ = c.Apply("user-1", now()) // UsedCount=1, Status=EXHAUSTED

	c.Rollback()

	if c.UsedCount != 0 {
		t.Errorf("expected UsedCount=0 after rollback, got %d", c.UsedCount)
	}
	if c.Status != model.CouponStatusActive {
		t.Errorf("expected Status=ACTIVE after rollback, got %s", c.Status)
	}
}

// ─────────────────────────────────────────────────────────────────
// IsApplicable: ACTIVE 状态前条件
// ─────────────────────────────────────────────────────────────────

func TestCoupon_IsApplicable_FalseWhenExpiredStatus(t *testing.T) {
	c := model.NewCoupon("EXP", validRule(), 5, past(), future())
	c.MarkExpired()

	if c.IsApplicable(now()) {
		t.Error("expected IsApplicable=false for EXPIRED status")
	}
}

func TestCoupon_IsApplicable_FalseBeforeValidFrom(t *testing.T) {
	startFuture := future()
	c := model.NewCoupon("FUTURE", validRule(), 0, startFuture, startFuture.Add(24*time.Hour))

	if c.IsApplicable(now()) {
		t.Error("expected IsApplicable=false before ValidFrom")
	}
}
