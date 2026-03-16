package model_test

import (
	"testing"
	"time"

	"payment-demo/internal/card/domain/event"
	"payment-demo/internal/card/domain/model"
)

// ─────────────────────────────────────────────────────────────────
// 测试辅助：构造一张标准 Active 卡
// ─────────────────────────────────────────────────────────────────

func newTestCard(userID string) *model.SavedCard {
	return model.NewSavedCard(
		userID,
		model.VaultToken{Token: "vault_tok_001", Provider: "mock"},
		model.CardMask{Last4: "4242", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
		model.CardHolder{Name: "Alice", BillingCountry: "US"},
	)
}

// ─────────────────────────────────────────────────────────────────
// AC-1  工厂方法 NewSavedCard
// ─────────────────────────────────────────────────────────────────

func TestNewSavedCard_InitialState(t *testing.T) {
	before := time.Now()
	card := newTestCard("user-1")
	after := time.Now()

	if card.Status != model.CardStatusActive {
		t.Errorf("want Status=ACTIVE, got %s", card.Status)
	}
	if card.IsDefault {
		t.Error("want IsDefault=false, got true")
	}
	if card.ID == "" {
		t.Error("want non-empty ID")
	}
	if len(card.Events) != 0 {
		t.Errorf("want 0 events after NewSavedCard, got %d", len(card.Events))
	}
	if card.CreatedAt.IsZero() || card.CreatedAt.Before(before) || card.CreatedAt.After(after) {
		t.Error("CreatedAt is not within expected range")
	}
	if card.UpdatedAt.IsZero() || card.UpdatedAt.Before(before) || card.UpdatedAt.After(after) {
		t.Error("UpdatedAt is not within expected range")
	}
}

func TestNewSavedCard_UniqueIDs(t *testing.T) {
	card1 := newTestCard("user-1")
	card2 := newTestCard("user-1")
	if card1.ID == card2.ID {
		t.Errorf("expected unique IDs, both got %s", card1.ID)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-2  BindAsDefault
// ─────────────────────────────────────────────────────────────────

func TestSavedCard_BindAsDefault_SetsDefaultAndPublishesEvent(t *testing.T) {
	card := newTestCard("user-1")
	card.BindAsDefault()

	if !card.IsDefault {
		t.Error("want IsDefault=true after BindAsDefault")
	}
	if len(card.Events) != 1 {
		t.Fatalf("want 1 event, got %d", len(card.Events))
	}
	if card.Events[0].EventName() != "card.bound" {
		t.Errorf("want event 'card.bound', got '%s'", card.Events[0].EventName())
	}
	bound, ok := card.Events[0].(event.CardBound)
	if !ok {
		t.Fatal("event is not CardBound")
	}
	if bound.CardID != string(card.ID) {
		t.Errorf("CardBound.CardID: want %s, got %s", card.ID, bound.CardID)
	}
	if bound.UserID != card.UserID {
		t.Errorf("CardBound.UserID: want %s, got %s", card.UserID, bound.UserID)
	}
	if bound.Last4 != card.Mask.Last4 {
		t.Errorf("CardBound.Last4: want %s, got %s", card.Mask.Last4, bound.Last4)
	}
	if !bound.IsDefault {
		t.Error("CardBound.IsDefault: want true")
	}
}

// ─────────────────────────────────────────────────────────────────
// Bind（非默认绑卡）
// ─────────────────────────────────────────────────────────────────

func TestSavedCard_Bind_PublishesCardBoundEventWithIsDefaultFalse(t *testing.T) {
	card := newTestCard("user-1")
	card.Bind()

	if card.IsDefault {
		t.Error("Bind must not set IsDefault")
	}
	if len(card.Events) != 1 {
		t.Fatalf("want 1 event, got %d", len(card.Events))
	}
	bound, ok := card.Events[0].(event.CardBound)
	if !ok {
		t.Fatal("event is not CardBound")
	}
	if bound.IsDefault {
		t.Error("CardBound.IsDefault should be false for non-default bind")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-3  Suspend：Active → Suspended（正常路径）
// ─────────────────────────────────────────────────────────────────

func TestSavedCard_Suspend_FromActive_Succeeds(t *testing.T) {
	card := newTestCard("user-1")
	prevUpdated := card.UpdatedAt

	time.Sleep(time.Millisecond) // 确保 UpdatedAt 可感知变化
	if err := card.Suspend(); err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if card.Status != model.CardStatusSuspended {
		t.Errorf("want SUSPENDED, got %s", card.Status)
	}
	if !card.UpdatedAt.After(prevUpdated) {
		t.Error("UpdatedAt should be updated after Suspend")
	}
	if len(card.Events) != 1 {
		t.Fatalf("want 1 event, got %d", len(card.Events))
	}
	if card.Events[0].EventName() != "card.suspended" {
		t.Errorf("want 'card.suspended', got '%s'", card.Events[0].EventName())
	}
	suspended, ok := card.Events[0].(event.CardSuspended)
	if !ok {
		t.Fatal("event is not CardSuspended")
	}
	if suspended.CardID != string(card.ID) {
		t.Errorf("CardSuspended.CardID: want %s, got %s", card.ID, suspended.CardID)
	}
	if suspended.UserID != card.UserID {
		t.Errorf("CardSuspended.UserID: want %s, got %s", card.UserID, suspended.UserID)
	}
}

// AC-4  Suspend：Suspended → 返回 ErrInvalidStateTransition
func TestSavedCard_Suspend_FromSuspended_ReturnsError(t *testing.T) {
	card := newTestCard("user-1")
	_ = card.Suspend() // Active → Suspended
	card.ClearEvents()

	err := card.Suspend()
	if err != model.ErrInvalidStateTransition {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
	if card.Status != model.CardStatusSuspended {
		t.Error("Status must remain SUSPENDED")
	}
	if len(card.Events) != 0 {
		t.Errorf("want no new events, got %d", len(card.Events))
	}
}

// AC-5  Suspend：Deleted → 返回 ErrInvalidStateTransition
func TestSavedCard_Suspend_FromDeleted_ReturnsError(t *testing.T) {
	card := newTestCard("user-1")
	_ = card.Delete()
	card.ClearEvents()

	err := card.Suspend()
	if err != model.ErrInvalidStateTransition {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
	if card.Status != model.CardStatusDeleted {
		t.Error("Status must remain DELETED")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-6  Activate：Suspended → Active（正常路径）
// ─────────────────────────────────────────────────────────────────

func TestSavedCard_Activate_FromSuspended_Succeeds(t *testing.T) {
	card := newTestCard("user-1")
	_ = card.Suspend()
	card.ClearEvents()
	prevUpdated := card.UpdatedAt

	time.Sleep(time.Millisecond)
	if err := card.Activate(); err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if card.Status != model.CardStatusActive {
		t.Errorf("want ACTIVE, got %s", card.Status)
	}
	if !card.UpdatedAt.After(prevUpdated) {
		t.Error("UpdatedAt should be updated after Activate")
	}
	// W-1: Activate 现在会发布 CardActivated 事件
	if len(card.Events) != 1 {
		t.Fatalf("want 1 CardActivated event after Activate, got %d", len(card.Events))
	}
	if card.Events[0].EventName() != "card.activated" {
		t.Errorf("want event 'card.activated', got '%s'", card.Events[0].EventName())
	}
	activated, ok := card.Events[0].(event.CardActivated)
	if !ok {
		t.Fatal("event is not CardActivated")
	}
	if activated.CardID != string(card.ID) {
		t.Errorf("CardActivated.CardID: want %s, got %s", card.ID, activated.CardID)
	}
	if activated.UserID != card.UserID {
		t.Errorf("CardActivated.UserID: want %s, got %s", card.UserID, activated.UserID)
	}
}

// AC-7  Activate：Active → 返回 ErrInvalidStateTransition
func TestSavedCard_Activate_FromActive_ReturnsError(t *testing.T) {
	card := newTestCard("user-1")

	err := card.Activate()
	if err != model.ErrInvalidStateTransition {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
	if card.Status != model.CardStatusActive {
		t.Error("Status must remain ACTIVE")
	}
}

// AC-8  Activate：Deleted → 返回 ErrInvalidStateTransition
func TestSavedCard_Activate_FromDeleted_ReturnsError(t *testing.T) {
	card := newTestCard("user-1")
	_ = card.Delete()
	card.ClearEvents()

	err := card.Activate()
	if err != model.ErrInvalidStateTransition {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
	if card.Status != model.CardStatusDeleted {
		t.Error("Status must remain DELETED")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-9  Delete：Active → Deleted（正常路径）
// ─────────────────────────────────────────────────────────────────

func TestSavedCard_Delete_FromActive_Succeeds(t *testing.T) {
	card := newTestCard("user-1")
	card.BindAsDefault() // IsDefault = true
	card.ClearEvents()
	prevUpdated := card.UpdatedAt

	time.Sleep(time.Millisecond)
	if err := card.Delete(); err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if card.Status != model.CardStatusDeleted {
		t.Errorf("want DELETED, got %s", card.Status)
	}
	if card.IsDefault {
		t.Error("Delete must clear IsDefault")
	}
	if !card.UpdatedAt.After(prevUpdated) {
		t.Error("UpdatedAt should be updated after Delete")
	}
	if len(card.Events) != 1 {
		t.Fatalf("want 1 event, got %d", len(card.Events))
	}
	if card.Events[0].EventName() != "card.deleted" {
		t.Errorf("want 'card.deleted', got '%s'", card.Events[0].EventName())
	}
	deleted, ok := card.Events[0].(event.CardDeleted)
	if !ok {
		t.Fatal("event is not CardDeleted")
	}
	if deleted.CardID != string(card.ID) {
		t.Errorf("CardDeleted.CardID: want %s, got %s", card.ID, deleted.CardID)
	}
	if deleted.UserID != card.UserID {
		t.Errorf("CardDeleted.UserID: want %s, got %s", card.UserID, deleted.UserID)
	}
}

// AC-10  Delete：Suspended → Deleted（正常路径）
func TestSavedCard_Delete_FromSuspended_Succeeds(t *testing.T) {
	card := newTestCard("user-1")
	_ = card.Suspend()
	card.ClearEvents()

	if err := card.Delete(); err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if card.Status != model.CardStatusDeleted {
		t.Errorf("want DELETED, got %s", card.Status)
	}
	if len(card.Events) != 1 || card.Events[0].EventName() != "card.deleted" {
		t.Error("want exactly 1 card.deleted event")
	}
}

// AC-11  Delete：Deleted → 返回 ErrInvalidStateTransition
func TestSavedCard_Delete_FromDeleted_ReturnsError(t *testing.T) {
	card := newTestCard("user-1")
	_ = card.Delete()
	card.ClearEvents()

	err := card.Delete()
	if err != model.ErrInvalidStateTransition {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
	if card.Status != model.CardStatusDeleted {
		t.Error("Status must remain DELETED")
	}
	if len(card.Events) != 0 {
		t.Errorf("want no new events, got %d", len(card.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-12  SetDefault：Active → IsDefault=true + 事件
// ─────────────────────────────────────────────────────────────────

func TestSavedCard_SetDefault_FromActive_Succeeds(t *testing.T) {
	card := newTestCard("user-1")
	prevUpdated := card.UpdatedAt

	time.Sleep(time.Millisecond)
	if err := card.SetDefault(); err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if !card.IsDefault {
		t.Error("want IsDefault=true after SetDefault")
	}
	if !card.UpdatedAt.After(prevUpdated) {
		t.Error("UpdatedAt should be updated after SetDefault")
	}
	if len(card.Events) != 1 {
		t.Fatalf("want 1 event, got %d", len(card.Events))
	}
	if card.Events[0].EventName() != "card.default_changed" {
		t.Errorf("want 'card.default_changed', got '%s'", card.Events[0].EventName())
	}
	changed, ok := card.Events[0].(event.DefaultCardChanged)
	if !ok {
		t.Fatal("event is not DefaultCardChanged")
	}
	if changed.CardID != string(card.ID) {
		t.Errorf("DefaultCardChanged.CardID: want %s, got %s", card.ID, changed.CardID)
	}
	if changed.UserID != card.UserID {
		t.Errorf("DefaultCardChanged.UserID: want %s, got %s", card.UserID, changed.UserID)
	}
}

// AC-13  SetDefault：Suspended → ErrCardNotUsable
func TestSavedCard_SetDefault_FromSuspended_ReturnsErrCardNotUsable(t *testing.T) {
	card := newTestCard("user-1")
	_ = card.Suspend()
	card.ClearEvents()
	wasDefault := card.IsDefault

	err := card.SetDefault()
	if err != model.ErrCardNotUsable {
		t.Errorf("want ErrCardNotUsable, got %v", err)
	}
	if card.IsDefault != wasDefault {
		t.Error("IsDefault must not change on error")
	}
	if len(card.Events) != 0 {
		t.Errorf("want no events, got %d", len(card.Events))
	}
}

// AC-14  SetDefault：Deleted → ErrCardNotUsable
func TestSavedCard_SetDefault_FromDeleted_ReturnsErrCardNotUsable(t *testing.T) {
	card := newTestCard("user-1")
	_ = card.Delete()
	card.ClearEvents()

	err := card.SetDefault()
	if err != model.ErrCardNotUsable {
		t.Errorf("want ErrCardNotUsable, got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-15  UnsetDefault
// ─────────────────────────────────────────────────────────────────

func TestSavedCard_UnsetDefault_ClearsFlag_NoEvent(t *testing.T) {
	card := newTestCard("user-1")
	card.BindAsDefault() // IsDefault = true
	card.ClearEvents()
	prevUpdated := card.UpdatedAt

	time.Sleep(time.Millisecond)
	card.UnsetDefault()

	if card.IsDefault {
		t.Error("want IsDefault=false after UnsetDefault")
	}
	if !card.UpdatedAt.After(prevUpdated) {
		t.Error("UpdatedAt should be updated after UnsetDefault")
	}
	if len(card.Events) != 0 {
		t.Errorf("UnsetDefault must not emit events, got %d", len(card.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-16  ClearEvents
// ─────────────────────────────────────────────────────────────────

func TestSavedCard_ClearEvents_ReturnsAndClearsAll(t *testing.T) {
	card := newTestCard("user-1")
	_ = card.Suspend()    // → card.suspended
	_ = card.Delete()     // → card.deleted（Suspend 已将状态置为 SUSPENDED，Delete 合法）

	events := card.ClearEvents()

	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d", len(events))
	}
	if events[0].EventName() != "card.suspended" {
		t.Errorf("events[0]: want 'card.suspended', got '%s'", events[0].EventName())
	}
	if events[1].EventName() != "card.deleted" {
		t.Errorf("events[1]: want 'card.deleted', got '%s'", events[1].EventName())
	}
	// 调用后内部列表应清空
	if len(card.Events) != 0 {
		t.Errorf("want empty Events after ClearEvents, got %d", len(card.Events))
	}
	// 再次调用应返回空切片
	second := card.ClearEvents()
	if len(second) != 0 {
		t.Errorf("second ClearEvents: want 0 events, got %d", len(second))
	}
}

// ─────────────────────────────────────────────────────────────────
// 值对象相等性
// ─────────────────────────────────────────────────────────────────

func TestVaultToken_Equality(t *testing.T) {
	a := model.VaultToken{Token: "tok_abc", Provider: "stripe"}
	b := model.VaultToken{Token: "tok_abc", Provider: "stripe"}
	c := model.VaultToken{Token: "tok_xyz", Provider: "stripe"}

	if a != b {
		t.Error("identical VaultTokens should be equal")
	}
	if a == c {
		t.Error("different VaultTokens should not be equal")
	}
}

func TestCardMask_Equality(t *testing.T) {
	a := model.CardMask{Last4: "4242", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028}
	b := model.CardMask{Last4: "4242", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028}
	c := model.CardMask{Last4: "1234", Brand: "Mastercard", ExpireMonth: 6, ExpireYear: 2030}

	if a != b {
		t.Error("identical CardMasks should be equal")
	}
	if a == c {
		t.Error("different CardMasks should not be equal")
	}
}

func TestSavedCardID_NewIsUnique(t *testing.T) {
	const n = 100
	ids := make(map[model.SavedCardID]bool, n)
	for i := 0; i < n; i++ {
		id := model.NewSavedCardID()
		if ids[id] {
			t.Fatalf("duplicate SavedCardID generated: %s", id)
		}
		ids[id] = true
	}
}
