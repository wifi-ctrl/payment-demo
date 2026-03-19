package persistence_test

import (
	"context"
	"sync"
	"testing"

	"payment-demo/internal/card/adapter/persistence"
	"payment-demo/internal/card/domain/model"
)

// ─────────────────────────────────────────────────────────────────
// 辅助
// ─────────────────────────────────────────────────────────────────

func newCard(userID string) *model.SavedCard {
	return model.NewSavedCard(
		userID,
		model.EncryptedPAN{Ciphertext: []byte("mock-cipher"), KeyVersion: 1},
		model.PANHash("mock-hash-4242"),
		model.CardMask{Last4: "4242", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
		model.CardHolder{Name: "Alice", BillingCountry: "US"},
	)
}

// ─────────────────────────────────────────────────────────────────
// 基础 CRUD
// ─────────────────────────────────────────────────────────────────

func TestInMemoryCardRepository_SaveAndFindByID(t *testing.T) {
	repo := persistence.NewInMemoryCardRepository()
	ctx := context.Background()
	card := newCard("user-1")

	if err := repo.Save(ctx, card); err != nil {
		t.Fatalf("Save: unexpected error: %v", err)
	}

	found, err := repo.FindByID(ctx, card.ID)
	if err != nil {
		t.Fatalf("FindByID: unexpected error: %v", err)
	}
	if found.ID != card.ID {
		t.Errorf("want ID=%s, got %s", card.ID, found.ID)
	}
}

func TestInMemoryCardRepository_FindByID_NotFound(t *testing.T) {
	repo := persistence.NewInMemoryCardRepository()
	_, err := repo.FindByID(context.Background(), "non-existent")
	if err != model.ErrCardNotFound {
		t.Errorf("want ErrCardNotFound, got %v", err)
	}
}

func TestInMemoryCardRepository_Save_Upsert(t *testing.T) {
	repo := persistence.NewInMemoryCardRepository()
	ctx := context.Background()
	card := newCard("user-1")

	_ = repo.Save(ctx, card)

	// 修改状态后再次保存（upsert）
	_ = card.Suspend()
	_ = repo.Save(ctx, card)

	found, _ := repo.FindByID(ctx, card.ID)
	if found.Status != model.CardStatusSuspended {
		t.Errorf("want SUSPENDED after upsert, got %s", found.Status)
	}
}

// ─────────────────────────────────────────────────────────────────
// FindAllByUserID — 不含 DELETED
// ─────────────────────────────────────────────────────────────────

func TestInMemoryCardRepository_FindAllByUserID_ExcludesDeleted(t *testing.T) {
	repo := persistence.NewInMemoryCardRepository()
	ctx := context.Background()

	active1 := newCard("user-1")
	active2 := newCard("user-1")
	deleted := newCard("user-1")
	_ = deleted.Delete()
	deleted.ClearEvents()
	other := newCard("other-user")

	for _, c := range []*model.SavedCard{active1, active2, deleted, other} {
		_ = repo.Save(ctx, c)
	}

	cards, err := repo.FindAllByUserID(ctx, "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cards) != 2 {
		t.Errorf("want 2 non-deleted cards for user-1, got %d", len(cards))
	}
}

func TestInMemoryCardRepository_FindAllByUserID_Empty(t *testing.T) {
	repo := persistence.NewInMemoryCardRepository()
	cards, err := repo.FindAllByUserID(context.Background(), "user-nobody")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 无数据时应返回 nil 或空切片，不报错
	if len(cards) != 0 {
		t.Errorf("want 0 cards, got %d", len(cards))
	}
}

// ─────────────────────────────────────────────────────────────────
// FindDefaultByUserID
// ─────────────────────────────────────────────────────────────────

func TestInMemoryCardRepository_FindDefaultByUserID_ReturnsDefault(t *testing.T) {
	repo := persistence.NewInMemoryCardRepository()
	ctx := context.Background()

	nonDefault := newCard("user-1")
	defaultCard := newCard("user-1")
	defaultCard.BindAsDefault()
	defaultCard.ClearEvents()

	_ = repo.Save(ctx, nonDefault)
	_ = repo.Save(ctx, defaultCard)

	found, err := repo.FindDefaultByUserID(ctx, "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found == nil || found.ID != defaultCard.ID {
		t.Errorf("want default card ID=%s, got %v", defaultCard.ID, found)
	}
}

func TestInMemoryCardRepository_FindDefaultByUserID_NilWhenNone(t *testing.T) {
	repo := persistence.NewInMemoryCardRepository()
	found, err := repo.FindDefaultByUserID(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != nil {
		t.Errorf("want nil when no default card, got %v", found)
	}
}

func TestInMemoryCardRepository_FindDefaultByUserID_ExcludesDeleted(t *testing.T) {
	repo := persistence.NewInMemoryCardRepository()
	ctx := context.Background()

	// 创建一张默认卡然后软删除
	card := newCard("user-1")
	card.BindAsDefault()
	card.ClearEvents()
	_ = repo.Save(ctx, card)
	_ = card.Delete()
	card.ClearEvents()
	_ = repo.Save(ctx, card) // 更新为 DELETED

	found, err := repo.FindDefaultByUserID(ctx, "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != nil {
		t.Error("deleted default card should not be returned by FindDefaultByUserID")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-28  并发写入不同卡 — race detector
// ─────────────────────────────────────────────────────────────────

func TestInMemoryCardRepository_ConcurrentSaveDifferentCards_NoRace(t *testing.T) {
	const n = 50
	repo := persistence.NewInMemoryCardRepository()
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			card := newCard("user-concurrent")
			if err := repo.Save(ctx, card); err != nil {
				t.Errorf("concurrent Save failed: %v", err)
			}
		}()
	}
	wg.Wait()

	cards, err := repo.FindAllByUserID(ctx, "user-concurrent")
	if err != nil {
		t.Fatalf("FindAllByUserID: unexpected error: %v", err)
	}
	if len(cards) != n {
		t.Errorf("want %d cards after concurrent writes, got %d", n, len(cards))
	}
}

// AC-29  并发读写同一卡 — race detector
func TestInMemoryCardRepository_ConcurrentReadWrite_NoRace(t *testing.T) {
	const n = 20
	repo := persistence.NewInMemoryCardRepository()
	ctx := context.Background()

	card := newCard("user-rw")
	_ = repo.Save(ctx, card)

	var wg sync.WaitGroup
	wg.Add(n * 2)

	// 并发读
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = repo.FindByID(ctx, card.ID) // 不在乎是否找到，不应 panic
		}()
	}
	// 并发写（覆盖同一卡）
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = repo.Save(ctx, card)
		}()
	}
	wg.Wait()
}
