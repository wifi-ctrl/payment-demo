package inmem_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"payment-demo/internal/coupon/adapter/inmem"
	"payment-demo/internal/coupon/domain/model"
)

// ─────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────

func newTestCoupon(code string) *model.Coupon {
	return model.NewCoupon(
		model.CouponCode(code),
		model.DiscountRule{Type: model.DiscountTypeFixed, Value: 100},
		10,
		time.Now().Add(-time.Hour),
		time.Now().Add(24*time.Hour),
	)
}

// ─────────────────────────────────────────────────────────────────
// AC-40: InMemoryCouponRepository — Save & FindByCode
// ─────────────────────────────────────────────────────────────────

func TestInMemoryCouponRepository_SaveAndFindByCode(t *testing.T) {
	// AC-40
	repo := inmem.NewInMemoryCouponRepository()
	ctx := context.Background()
	c := newTestCoupon("SAVE10")

	if err := repo.Save(ctx, c); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := repo.FindByCode(ctx, "SAVE10")
	if err != nil {
		t.Fatalf("FindByCode failed: %v", err)
	}
	if got.ID != c.ID {
		t.Errorf("expected ID=%s, got %s", c.ID, got.ID)
	}
	if got.Code != c.Code {
		t.Errorf("expected Code=%s, got %s", c.Code, got.Code)
	}
	if got.MaxUses != c.MaxUses {
		t.Errorf("expected MaxUses=%d, got %d", c.MaxUses, got.MaxUses)
	}
	if got.Status != c.Status {
		t.Errorf("expected Status=%s, got %s", c.Status, got.Status)
	}
}

func TestInMemoryCouponRepository_FindByCode_NotFound(t *testing.T) {
	// AC-40
	repo := inmem.NewInMemoryCouponRepository()
	ctx := context.Background()

	_, err := repo.FindByCode(ctx, "NOTEXIST")
	if err != model.ErrCouponNotFound {
		t.Errorf("expected ErrCouponNotFound, got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-41: InMemoryCouponRepository — FindByID 不存在
// ─────────────────────────────────────────────────────────────────

func TestInMemoryCouponRepository_FindByID_NotFound(t *testing.T) {
	// AC-41
	repo := inmem.NewInMemoryCouponRepository()
	ctx := context.Background()

	_, err := repo.FindByID(ctx, "nonexistent-id")
	if err != model.ErrCouponNotFound {
		t.Errorf("expected ErrCouponNotFound, got %v", err)
	}
}

func TestInMemoryCouponRepository_SaveAndFindByID(t *testing.T) {
	repo := inmem.NewInMemoryCouponRepository()
	ctx := context.Background()
	c := newTestCoupon("BYID")

	_ = repo.Save(ctx, c)
	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}
	if got.ID != c.ID {
		t.Errorf("expected ID=%s, got %s", c.ID, got.ID)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-42: InMemoryCouponRepository — 并发 Save 无 race condition
// ─────────────────────────────────────────────────────────────────

func TestInMemoryCouponRepository_ConcurrentSave(t *testing.T) {
	// AC-42 — 运行时加 -race 检测
	repo := inmem.NewInMemoryCouponRepository()
	ctx := context.Background()

	var wg sync.WaitGroup
	coupons := make([]*model.Coupon, 50)
	for i := range coupons {
		coupons[i] = newTestCoupon(string(rune('A' + i%26)))
		// 为避免重复 code，给每个 coupon 唯一 ID (NewCoupon 已用 uuid)
		// 用不同 code 后缀
	}

	// 重新生成保证 code 唯一
	for i := range coupons {
		code := model.CouponCode("CONCURRENT" + string(rune('A'+i)))
		coupons[i] = model.NewCoupon(
			code,
			model.DiscountRule{Type: model.DiscountTypeFixed, Value: 100},
			0,
			time.Now().Add(-time.Hour),
			time.Now().Add(24*time.Hour),
		)
	}

	for _, c := range coupons {
		wg.Add(1)
		go func(c *model.Coupon) {
			defer wg.Done()
			_ = repo.Save(ctx, c)
		}(c)
	}
	wg.Wait()

	// 验证所有 coupon 均可通过 FindByID 查到
	for _, c := range coupons {
		got, err := repo.FindByID(ctx, c.ID)
		if err != nil {
			t.Errorf("FindByID failed for %s: %v", c.ID, err)
		}
		if got == nil {
			t.Errorf("expected non-nil for ID=%s", c.ID)
		}
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-43: InMemoryCouponRepository — Save 覆盖更新（同 ID 二次 Save）
// ─────────────────────────────────────────────────────────────────

func TestInMemoryCouponRepository_SaveOverwrite(t *testing.T) {
	// AC-43
	repo := inmem.NewInMemoryCouponRepository()
	ctx := context.Background()

	c := newTestCoupon("UPSERT")
	_ = repo.Save(ctx, c) // 初始 UsedCount=0

	c.UsedCount = 1
	_ = repo.Save(ctx, c) // 更新 UsedCount=1

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}
	if got.UsedCount != 1 {
		t.Errorf("expected UsedCount=1 after overwrite, got %d", got.UsedCount)
	}
}
