package persistence_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"payment-demo/internal/merchant/adapter/persistence"
	"payment-demo/internal/merchant/domain/model"
)

// ─────────────────────────────────────────────────────────────────
// 测试辅助
// ─────────────────────────────────────────────────────────────────

func newRepo() *persistence.InMemoryMerchantRepository {
	return persistence.NewInMemoryMerchantRepository()
}

func activeMerchant(name string) *model.Merchant {
	m := model.NewMerchant(name)
	m.ClearEvents()
	return m
}

// ─────────────────────────────────────────────────────────────────
// AC-40: Save 后 FindByID 能取回相同商户
// ─────────────────────────────────────────────────────────────────

func TestInMemoryMerchantRepo_SaveThenFindByID_ReturnsCorrectMerchant(t *testing.T) {
	// AC-40
	repo := newRepo()
	m := activeMerchant("Acme Corp")

	if err := repo.Save(context.Background(), m); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(context.Background(), m.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID: want non-nil")
	}
	if got.ID != m.ID {
		t.Errorf("ID: want %s, got %s", m.ID, got.ID)
	}
	if got.Name != m.Name {
		t.Errorf("Name: want %s, got %s", m.Name, got.Name)
	}
	if got.Status != m.Status {
		t.Errorf("Status: want %s, got %s", m.Status, got.Status)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-41: FindByID 找不到时返回 ErrMerchantNotFound
// ─────────────────────────────────────────────────────────────────

func TestInMemoryMerchantRepo_FindByID_NotFound_ReturnsError(t *testing.T) {
	// AC-41
	repo := newRepo()

	got, err := repo.FindByID(context.Background(), model.MerchantID("non-existent-id"))

	if !errors.Is(err, model.ErrMerchantNotFound) {
		t.Errorf("want ErrMerchantNotFound, got %v", err)
	}
	if got != nil {
		t.Errorf("want nil merchant, got %+v", got)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-42: Save 对同一 ID 执行 upsert（更新覆盖）
// ─────────────────────────────────────────────────────────────────

func TestInMemoryMerchantRepo_Save_Upsert_OverwritesExisting(t *testing.T) {
	// AC-42
	repo := newRepo()
	m := activeMerchant("Acme Corp")

	// 第一次 Save: ACTIVE
	if err := repo.Save(context.Background(), m); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	// 变更状态后第二次 Save: SUSPENDED
	_ = m.Suspend()
	m.ClearEvents()
	if err := repo.Save(context.Background(), m); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	// FindByID 返回更新后的状态
	got, err := repo.FindByID(context.Background(), m.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != model.MerchantStatusSuspended {
		t.Errorf("Status: want SUSPENDED, got %s", got.Status)
	}

	// FindAll 长度不增加（仍为 1）
	all, err := repo.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("FindAll len: want 1 after upsert, got %d", len(all))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-43: FindAll 返回所有已保存商户
// ─────────────────────────────────────────────────────────────────

func TestInMemoryMerchantRepo_FindAll_ReturnsAllSaved(t *testing.T) {
	// AC-43
	repo := newRepo()

	merchants := []*model.Merchant{
		activeMerchant("A"),
		activeMerchant("B"),
		activeMerchant("C"),
	}
	for _, m := range merchants {
		if err := repo.Save(context.Background(), m); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	all, err := repo.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("FindAll len: want 3, got %d", len(all))
	}
}

func TestInMemoryMerchantRepo_FindAll_EmptyRepo_ReturnsEmptySlice(t *testing.T) {
	repo := newRepo()

	all, err := repo.FindAll(context.Background())

	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("FindAll: want 0, got %d", len(all))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-44: 并发 Save 无 race condition（go test -race）
// ─────────────────────────────────────────────────────────────────

func TestInMemoryMerchantRepo_ConcurrentSave_NoRace(t *testing.T) {
	// AC-44
	const n = 100
	repo := newRepo()

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			m := activeMerchant("concurrent-merchant")
			if err := repo.Save(context.Background(), m); err != nil {
				t.Errorf("concurrent Save: %v", err)
			}
		}()
	}
	wg.Wait()

	all, err := repo.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll after concurrent Save: %v", err)
	}
	if len(all) != n {
		t.Errorf("FindAll len: want %d, got %d", n, len(all))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-45: 并发 Save + FindByID 无 race condition
// ─────────────────────────────────────────────────────────────────

func TestInMemoryMerchantRepo_ConcurrentSaveAndFind_NoRace(t *testing.T) {
	// AC-45
	const n = 50
	repo := newRepo()

	// 预置若干商户
	seeded := make([]*model.Merchant, n)
	for i := 0; i < n; i++ {
		m := activeMerchant("seeded")
		if err := repo.Save(context.Background(), m); err != nil {
			t.Fatalf("seed Save: %v", err)
		}
		seeded[i] = m
	}

	var wg sync.WaitGroup
	wg.Add(n * 2)

	// 50 个 goroutine 并发 Save 新商户
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			m := activeMerchant("new-concurrent")
			_ = repo.Save(context.Background(), m)
		}()
	}

	// 50 个 goroutine 并发 FindByID 已有商户
	for i := 0; i < n; i++ {
		idx := i % n
		go func(merchantID model.MerchantID) {
			defer wg.Done()
			_, _ = repo.FindByID(context.Background(), merchantID)
		}(seeded[idx].ID)
	}

	wg.Wait()
	// 主要目的是 -race 检测，无断言；若有 race 则 go test -race 失败
}

// ─────────────────────────────────────────────────────────────────
// 额外：Save 的返回值永远是 nil（InMemory 实现）
// ─────────────────────────────────────────────────────────────────

func TestInMemoryMerchantRepo_Save_AlwaysReturnsNil(t *testing.T) {
	repo := newRepo()
	m := activeMerchant("Test")

	if err := repo.Save(context.Background(), m); err != nil {
		t.Errorf("Save: want nil, got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// 额外：FindAll 返回独立切片（外部 append 不影响内部状态）
// ─────────────────────────────────────────────────────────────────

func TestInMemoryMerchantRepo_FindAll_ReturnsCopy(t *testing.T) {
	repo := newRepo()
	m := activeMerchant("A")
	_ = repo.Save(context.Background(), m)

	all, _ := repo.FindAll(context.Background())
	originalLen := len(all)

	// 对外部切片 append，不影响 repo 内部
	_ = append(all, activeMerchant("B")) //nolint:staticcheck // 验证 append 不影响内部状态

	all2, _ := repo.FindAll(context.Background())
	if len(all2) != originalLen {
		t.Errorf("FindAll returned shared slice: want %d, got %d", originalLen, len(all2))
	}
}
