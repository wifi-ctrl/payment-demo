package card_test

import (
	"context"
	"testing"

	cardAdapter "payment-demo/internal/payment/adapter/card"
	"payment-demo/internal/card/adapter/persistence"
	cardModel "payment-demo/internal/card/domain/model"
	paymentModel "payment-demo/internal/payment/domain/model"
)

// ─────────────────────────────────────────────────────────────────
// 辅助
// ─────────────────────────────────────────────────────────────────

func seedActiveCard(t *testing.T, repo *persistence.InMemoryCardRepository, userID string) *cardModel.SavedCard {
	t.Helper()
	card := cardModel.NewSavedCard(
		userID,
		cardModel.VaultToken{Token: "tok_abc", Provider: "mock"},
		cardModel.CardMask{Last4: "4242", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
		cardModel.CardHolder{Name: "Alice", BillingCountry: "US"},
	)
	if err := repo.Save(context.Background(), card); err != nil {
		t.Fatalf("seed active card: %v", err)
	}
	return card
}

func seedSuspendedCard(t *testing.T, repo *persistence.InMemoryCardRepository, userID string) *cardModel.SavedCard {
	t.Helper()
	card := cardModel.NewSavedCard(
		userID,
		cardModel.VaultToken{Token: "tok_sus", Provider: "mock"},
		cardModel.CardMask{Last4: "9999", Brand: "Mastercard", ExpireMonth: 6, ExpireYear: 2026},
		cardModel.CardHolder{Name: "Bob", BillingCountry: "UK"},
	)
	_ = card.Suspend()
	card.ClearEvents()
	if err := repo.Save(context.Background(), card); err != nil {
		t.Fatalf("seed suspended card: %v", err)
	}
	return card
}

// ─────────────────────────────────────────────────────────────────
// AC-30  Active 卡 → 正确翻译为 SavedCardView
// ─────────────────────────────────────────────────────────────────

func TestCardAdapter_FindActiveCard_ActiveCard_ReturnsView(t *testing.T) {
	repo := persistence.NewInMemoryCardRepository()
	card := seedActiveCard(t, repo, "u1")

	adapter := cardAdapter.NewCardAdapter(repo)
	view, err := adapter.FindActiveCard(context.Background(), string(card.ID))
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if view == nil {
		t.Fatal("want non-nil SavedCardView, got nil")
	}

	// 字段逐一校验
	if view.CardID != string(card.ID) {
		t.Errorf("CardID: want %s, got %s", card.ID, view.CardID)
	}
	if view.UserID != "u1" {
		t.Errorf("UserID: want u1, got %s", view.UserID)
	}
	if view.Token != card.VaultToken.Token {
		t.Errorf("Token: want %s, got %s", card.VaultToken.Token, view.Token)
	}
	if view.Last4 != card.Mask.Last4 {
		t.Errorf("Last4: want %s, got %s", card.Mask.Last4, view.Last4)
	}
	if view.Brand != card.Mask.Brand {
		t.Errorf("Brand: want %s, got %s", card.Mask.Brand, view.Brand)
	}
	if !view.IsActive {
		t.Error("IsActive: want true for Active card")
	}
}

// AC-31  Suspended 卡 → IsActive=false
func TestCardAdapter_FindActiveCard_SuspendedCard_IsActiveFalse(t *testing.T) {
	repo := persistence.NewInMemoryCardRepository()
	card := seedSuspendedCard(t, repo, "u1")

	adapter := cardAdapter.NewCardAdapter(repo)
	view, err := adapter.FindActiveCard(context.Background(), string(card.ID))
	// adapter 目前返回 IsActive=false 而不一定返回 error，
	// 两种行为都符合 AC-31：要么 error，要么 IsActive==false
	if err != nil {
		// 返回错误也是合法行为
		return
	}
	if view == nil {
		t.Fatal("view should not be nil when no error returned")
	}
	if view.IsActive {
		t.Error("IsActive should be false for Suspended card")
	}
}

// AC-32  不存在的卡 ID → 返回 error
func TestCardAdapter_FindActiveCard_NotFound_ReturnsError(t *testing.T) {
	repo := persistence.NewInMemoryCardRepository()
	adapter := cardAdapter.NewCardAdapter(repo)

	view, err := adapter.FindActiveCard(context.Background(), "card-999")
	if err == nil {
		t.Fatal("want error for non-existent card ID, got nil")
	}
	if view != nil {
		t.Error("want nil view when error returned")
	}
	// 应返回 payment 上下文的 ErrCardNotFound
	if err != paymentModel.ErrCardNotFound {
		t.Errorf("want paymentModel.ErrCardNotFound, got %v", err)
	}
}
