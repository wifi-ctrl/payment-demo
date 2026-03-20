package card_test

import (
	"context"
	"testing"

	"payment-demo/internal/card/adapter/persistence"
	cardModel "payment-demo/internal/card/domain/model"
	cardAdapter "payment-demo/internal/acquiring/adapter/card"
	paymentModel "payment-demo/internal/acquiring/domain/model"
)

// ─────────────────────────────────────────────────────────────────
// 辅助
// ─────────────────────────────────────────────────────────────────

func seedActiveCard(t *testing.T, repo *persistence.InMemoryCardRepository, userID string) *cardModel.SavedCard {
	t.Helper()
	card := cardModel.NewSavedCard(
		userID,
		cardModel.EncryptedPAN{Ciphertext: []byte("enc:4242"), KeyVersion: 1},
		cardModel.PANHash("hmac:4242"),
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
		cardModel.EncryptedPAN{Ciphertext: []byte("enc:9999"), KeyVersion: 1},
		cardModel.PANHash("hmac:9999"),
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
// Active 卡 → 正确翻译为 SavedCardView
// ─────────────────────────────────────────────────────────────────

func TestCardAdapter_FindActiveCard_ActiveCard_ReturnsView(t *testing.T) {
	repo := persistence.NewInMemoryCardRepository()
	card := seedActiveCard(t, repo, "u1")
	card.StoreChannelToken("stripe", "pm_recurring_123", "shopper_1")
	_ = repo.Save(context.Background(), card)

	adapter := cardAdapter.NewCardAdapter(repo)
	view, err := adapter.FindActiveCard(context.Background(), string(card.ID))
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if view == nil {
		t.Fatal("want non-nil SavedCardView, got nil")
	}

	if view.CardID != string(card.ID) {
		t.Errorf("CardID: want %s, got %s", card.ID, view.CardID)
	}
	if view.UserID != "u1" {
		t.Errorf("UserID: want u1, got %s", view.UserID)
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
	if tok, ok := view.ChannelTokens["stripe"]; !ok || tok != "pm_recurring_123" {
		t.Errorf("ChannelTokens[stripe]: want pm_recurring_123, got %v", view.ChannelTokens)
	}
}

// Suspended 卡 → IsActive=false
func TestCardAdapter_FindActiveCard_SuspendedCard_IsActiveFalse(t *testing.T) {
	repo := persistence.NewInMemoryCardRepository()
	card := seedSuspendedCard(t, repo, "u1")

	adapter := cardAdapter.NewCardAdapter(repo)
	view, err := adapter.FindActiveCard(context.Background(), string(card.ID))
	if err != nil {
		return
	}
	if view == nil {
		t.Fatal("view should not be nil when no error returned")
	}
	if view.IsActive {
		t.Error("IsActive should be false for Suspended card")
	}
}

// 不存在的卡 ID → 返回 error
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
	if err != paymentModel.ErrCardNotFound {
		t.Errorf("want paymentModel.ErrCardNotFound, got %v", err)
	}
}
