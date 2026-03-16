package application_test

import (
	"context"
	"errors"
	"testing"

	"payment-demo/internal/card/application"
	"payment-demo/internal/card/domain/model"
	"payment-demo/internal/card/domain/port"
)

// ─────────────────────────────────────────────────────────────────
// Test Doubles（Stub / Spy）
// ─────────────────────────────────────────────────────────────────

// stubVault 可控制 Tokenize/Delete 的行为
type stubVault struct {
	tokenizeResult *port.VaultResult
	tokenizeErr    error
	deleteErr      error
	deleteCalled   bool
	deleteToken    model.VaultToken
}

func (v *stubVault) Tokenize(_ context.Context, _ string) (*port.VaultResult, error) {
	return v.tokenizeResult, v.tokenizeErr
}

func (v *stubVault) Delete(_ context.Context, token model.VaultToken) error {
	v.deleteCalled = true
	v.deleteToken = token
	return v.deleteErr
}

func okVaultResult() *port.VaultResult {
	return &port.VaultResult{
		VaultToken: model.VaultToken{Token: "vault_tok_abc", Provider: "mock"},
		Mask:       model.CardMask{Last4: "4242", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
		Holder:     model.CardHolder{Name: "Alice", BillingCountry: "US"},
	}
}

// stubRepo 可控制各查询返回的内存仓储 Stub
// 同时记录 Save 调用次数和参数
type stubRepo struct {
	byID        map[model.SavedCardID]*model.SavedCard // FindByID 的固定返回
	byIDErr     map[model.SavedCardID]error
	defaultCard *model.SavedCard // FindDefaultByUserID 的固定返回
	defaultErr  error
	allCards    []*model.SavedCard // FindAllByUserID 的固定返回
	saveErr     error
	savedCards  []*model.SavedCard // Save 被调用的所有入参
}

func newStubRepo() *stubRepo {
	return &stubRepo{
		byID:    make(map[model.SavedCardID]*model.SavedCard),
		byIDErr: make(map[model.SavedCardID]error),
	}
}

func (r *stubRepo) Save(_ context.Context, card *model.SavedCard) error {
	r.savedCards = append(r.savedCards, card)
	return r.saveErr
}

func (r *stubRepo) FindByID(_ context.Context, id model.SavedCardID) (*model.SavedCard, error) {
	if err, ok := r.byIDErr[id]; ok {
		return nil, err
	}
	if c, ok := r.byID[id]; ok {
		return c, nil
	}
	return nil, model.ErrCardNotFound
}

func (r *stubRepo) FindAllByUserID(_ context.Context, _ string) ([]*model.SavedCard, error) {
	return r.allCards, nil
}

func (r *stubRepo) FindDefaultByUserID(_ context.Context, _ string) (*model.SavedCard, error) {
	return r.defaultCard, r.defaultErr
}

func (r *stubRepo) saveCount() int {
	return len(r.savedCards)
}

// ─────────────────────────────────────────────────────────────────
// 辅助：构建 UseCase
// ─────────────────────────────────────────────────────────────────

func buildUseCase(repo port.CardRepository, vault port.CardVault) *application.CardUseCase {
	return application.NewCardUseCase(repo, vault)
}

func newActiveCard(userID string) *model.SavedCard {
	return model.NewSavedCard(
		userID,
		model.VaultToken{Token: "vault_tok_001", Provider: "mock"},
		model.CardMask{Last4: "4242", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
		model.CardHolder{Name: "Alice", BillingCountry: "US"},
	)
}

// ─────────────────────────────────────────────────────────────────
// AC-17  BindCard：首张卡自动成为默认
// ─────────────────────────────────────────────────────────────────

func TestCardUseCase_BindCard_FirstCard_BecomesDefault(t *testing.T) {
	vault := &stubVault{tokenizeResult: okVaultResult()}
	repo := newStubRepo()
	// repo.defaultCard == nil => 无已有默认卡

	uc := buildUseCase(repo, vault)
	card, err := uc.BindCard(context.Background(), application.BindCardRequest{
		UserID:       "user-1",
		OneTimeToken: "tok_frontend_xyz",
	})
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}

	// 卡 ID 非空
	if card.ID == "" {
		t.Error("card.ID must not be empty")
	}
	// 首张卡应为默认
	if !card.IsDefault {
		t.Error("first card must be default (IsDefault=true)")
	}
	// 状态为 Active
	if card.Status != model.CardStatusActive {
		t.Errorf("want ACTIVE, got %s", card.Status)
	}
	// Save 被调用一次
	if repo.saveCount() != 1 {
		t.Errorf("want Save called 1 time, got %d", repo.saveCount())
	}
	// publishEvents 已清空事件
	if len(card.Events) != 0 {
		t.Errorf("events should be cleared after BindCard, got %d", len(card.Events))
	}
}

// AC-18  BindCard：第二张卡不影响已有默认卡
func TestCardUseCase_BindCard_SecondCard_DoesNotChangeDefault(t *testing.T) {
	vault := &stubVault{tokenizeResult: okVaultResult()}
	repo := newStubRepo()
	existingDefault := newActiveCard("user-1")
	existingDefault.BindAsDefault()
	repo.defaultCard = existingDefault

	uc := buildUseCase(repo, vault)
	card, err := uc.BindCard(context.Background(), application.BindCardRequest{
		UserID:       "user-1",
		OneTimeToken: "tok_second",
	})
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if card.IsDefault {
		t.Error("second card must NOT be default")
	}
	// 旧默认卡的 IsDefault 不应改变
	if !existingDefault.IsDefault {
		t.Error("existing default card must remain default")
	}
	// Save 只被调用一次（仅新卡）
	if repo.saveCount() != 1 {
		t.Errorf("want Save called 1 time, got %d", repo.saveCount())
	}
	// 事件中 IsDefault 为 false
	if len(card.Events) != 0 {
		t.Errorf("events should be cleared, got %d", len(card.Events))
	}
}

// AC-19  BindCard：Vault Tokenize 失败 → 不持久化
func TestCardUseCase_BindCard_VaultFails_DoesNotSave(t *testing.T) {
	vault := &stubVault{tokenizeErr: errors.New("vault timeout")}
	repo := newStubRepo()

	uc := buildUseCase(repo, vault)
	_, err := uc.BindCard(context.Background(), application.BindCardRequest{
		UserID:       "user-1",
		OneTimeToken: "tok_fail_xyz",
	})
	if err == nil {
		t.Fatal("want error when Vault fails, got nil")
	}
	if repo.saveCount() != 0 {
		t.Errorf("Save must not be called when Vault fails, called %d times", repo.saveCount())
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-20  SuspendCard：正常挂起自己的卡
// ─────────────────────────────────────────────────────────────────

func TestCardUseCase_SuspendCard_OwnActiveCard_Succeeds(t *testing.T) {
	vault := &stubVault{}
	repo := newStubRepo()
	card := newActiveCard("user-1")
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, vault)
	result, err := uc.SuspendCard(context.Background(), "user-1", card.ID)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if result.Status != model.CardStatusSuspended {
		t.Errorf("want SUSPENDED, got %s", result.Status)
	}
	if repo.saveCount() != 1 {
		t.Errorf("want Save called 1 time, got %d", repo.saveCount())
	}
	// events 已被 publishEvents 清空
	if len(result.Events) != 0 {
		t.Errorf("events should be cleared, got %d", len(result.Events))
	}
}

// AC-21  SuspendCard：操作他人的卡 → ErrCardBelongsToOtherUser
func TestCardUseCase_SuspendCard_OtherUserCard_ReturnsForbidden(t *testing.T) {
	vault := &stubVault{}
	repo := newStubRepo()
	card := newActiveCard("other-user")
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, vault)
	_, err := uc.SuspendCard(context.Background(), "user-1", card.ID)
	if !errors.Is(err, model.ErrCardBelongsToOtherUser) {
		t.Errorf("want ErrCardBelongsToOtherUser, got %v", err)
	}
	if repo.saveCount() != 0 {
		t.Errorf("Save must not be called, got %d", repo.saveCount())
	}
	if card.Status != model.CardStatusActive {
		t.Error("card.Status must remain ACTIVE")
	}
}

// AC-22  SuspendCard：卡不存在 → ErrCardNotFound
func TestCardUseCase_SuspendCard_NotFound_ReturnsError(t *testing.T) {
	vault := &stubVault{}
	repo := newStubRepo()

	uc := buildUseCase(repo, vault)
	_, err := uc.SuspendCard(context.Background(), "user-1", "non-existent-id")
	if !errors.Is(err, model.ErrCardNotFound) {
		t.Errorf("want ErrCardNotFound, got %v", err)
	}
	if repo.saveCount() != 0 {
		t.Errorf("Save must not be called, got %d", repo.saveCount())
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-23  DeleteCard：删除 Active 卡，Vault 同步删除
// ─────────────────────────────────────────────────────────────────

func TestCardUseCase_DeleteCard_ActiveCard_CallsVaultAndSaves(t *testing.T) {
	vault := &stubVault{}
	repo := newStubRepo()
	card := newActiveCard("user-1")
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, vault)
	err := uc.DeleteCard(context.Background(), "user-1", card.ID)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if card.Status != model.CardStatusDeleted {
		t.Errorf("want DELETED, got %s", card.Status)
	}
	if repo.saveCount() != 1 {
		t.Errorf("want Save called 1 time, got %d", repo.saveCount())
	}
	if !vault.deleteCalled {
		t.Error("Vault.Delete must be called on card deletion")
	}
	if vault.deleteToken != card.VaultToken {
		t.Errorf("Vault.Delete token mismatch: want %+v, got %+v", card.VaultToken, vault.deleteToken)
	}
	// events 已清空
	if len(card.Events) != 0 {
		t.Errorf("events should be cleared, got %d", len(card.Events))
	}
}

// AC-24  DeleteCard：Deleted 状态 → ErrInvalidStateTransition，Vault 不被调用
func TestCardUseCase_DeleteCard_AlreadyDeleted_ReturnsError(t *testing.T) {
	vault := &stubVault{}
	repo := newStubRepo()
	card := newActiveCard("user-1")
	_ = card.Delete()
	card.ClearEvents()
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, vault)
	err := uc.DeleteCard(context.Background(), "user-1", card.ID)
	if !errors.Is(err, model.ErrInvalidStateTransition) {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
	if vault.deleteCalled {
		t.Error("Vault.Delete must NOT be called when card already deleted")
	}
	if repo.saveCount() != 0 {
		t.Errorf("Save must NOT be called, got %d", repo.saveCount())
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-25  SetDefaultCard：切换默认卡，旧卡取消默认
// ─────────────────────────────────────────────────────────────────

func TestCardUseCase_SetDefaultCard_SwitchesDefault(t *testing.T) {
	vault := &stubVault{}
	repo := newStubRepo()

	oldCard := newActiveCard("user-1")
	oldCard.BindAsDefault() // IsDefault = true
	oldCard.ClearEvents()

	newCard := newActiveCard("user-1")
	repo.byID[oldCard.ID] = oldCard
	repo.byID[newCard.ID] = newCard
	repo.defaultCard = oldCard // FindDefaultByUserID 返回旧卡

	uc := buildUseCase(repo, vault)
	result, err := uc.SetDefaultCard(context.Background(), "user-1", newCard.ID)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}

	if !result.IsDefault {
		t.Error("new card must be default")
	}
	if oldCard.IsDefault {
		t.Error("old card must no longer be default")
	}
	// Save 被调用两次（oldCard + newCard）
	if repo.saveCount() != 2 {
		t.Errorf("want Save called 2 times, got %d", repo.saveCount())
	}
	// DefaultCardChanged 事件已清空（publishEvents 已调用）
	if len(result.Events) != 0 {
		t.Errorf("events should be cleared, got %d", len(result.Events))
	}
}

// AC-26  SetDefaultCard：当前无默认卡，直接设置
func TestCardUseCase_SetDefaultCard_NoExistingDefault_SetsDirectly(t *testing.T) {
	vault := &stubVault{}
	repo := newStubRepo()

	targetCard := newActiveCard("user-1")
	repo.byID[targetCard.ID] = targetCard
	repo.defaultCard = nil // 无默认卡

	uc := buildUseCase(repo, vault)
	result, err := uc.SetDefaultCard(context.Background(), "user-1", targetCard.ID)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if !result.IsDefault {
		t.Error("target card must be default")
	}
	// Save 只被调用一次（仅目标卡）
	if repo.saveCount() != 1 {
		t.Errorf("want Save called 1 time, got %d", repo.saveCount())
	}
}

// AC-27  SetDefaultCard：Suspended 卡 → ErrCardNotUsable
func TestCardUseCase_SetDefaultCard_SuspendedCard_ReturnsError(t *testing.T) {
	vault := &stubVault{}
	repo := newStubRepo()

	suspendedCard := newActiveCard("user-1")
	_ = suspendedCard.Suspend()
	suspendedCard.ClearEvents()
	repo.byID[suspendedCard.ID] = suspendedCard
	repo.defaultCard = nil

	uc := buildUseCase(repo, vault)
	_, err := uc.SetDefaultCard(context.Background(), "user-1", suspendedCard.ID)
	if !errors.Is(err, model.ErrCardNotUsable) {
		t.Errorf("want ErrCardNotUsable, got %v", err)
	}
	if repo.saveCount() != 0 {
		t.Errorf("Save must not be called, got %d", repo.saveCount())
	}
}

// ─────────────────────────────────────────────────────────────────
// SetDefaultCard：对已经是默认卡的同一张卡不重复 UnsetDefault
// ─────────────────────────────────────────────────────────────────

func TestCardUseCase_SetDefaultCard_SameCard_DoesNotUnsetSelf(t *testing.T) {
	vault := &stubVault{}
	repo := newStubRepo()

	card := newActiveCard("user-1")
	card.BindAsDefault()
	card.ClearEvents()
	repo.byID[card.ID] = card
	repo.defaultCard = card // 当前默认卡就是目标卡

	uc := buildUseCase(repo, vault)
	result, err := uc.SetDefaultCard(context.Background(), "user-1", card.ID)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if !result.IsDefault {
		t.Error("card must remain default")
	}
	// 只保存一次（旧卡==新卡，跳过旧卡 UnsetDefault+Save）
	if repo.saveCount() != 1 {
		t.Errorf("want Save called 1 time, got %d", repo.saveCount())
	}
}

// ─────────────────────────────────────────────────────────────────
// ListCards：只返回未删除的卡
// ─────────────────────────────────────────────────────────────────

func TestCardUseCase_ListCards_ReturnsFromRepo(t *testing.T) {
	vault := &stubVault{}
	repo := newStubRepo()
	repo.allCards = []*model.SavedCard{newActiveCard("user-1"), newActiveCard("user-1")}

	uc := buildUseCase(repo, vault)
	cards, err := uc.ListCards(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if len(cards) != 2 {
		t.Errorf("want 2 cards, got %d", len(cards))
	}
}

// ─────────────────────────────────────────────────────────────────
// ActivateCard：激活挂起的卡
// ─────────────────────────────────────────────────────────────────

func TestCardUseCase_ActivateCard_FromSuspended_Succeeds(t *testing.T) {
	vault := &stubVault{}
	repo := newStubRepo()
	card := newActiveCard("user-1")
	_ = card.Suspend()
	card.ClearEvents()
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, vault)
	result, err := uc.ActivateCard(context.Background(), "user-1", card.ID)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if result.Status != model.CardStatusActive {
		t.Errorf("want ACTIVE, got %s", result.Status)
	}
	if repo.saveCount() != 1 {
		t.Errorf("want Save called 1 time, got %d", repo.saveCount())
	}
	if len(result.Events) != 0 {
		t.Errorf("events should be cleared, got %d", len(result.Events))
	}
}

func TestCardUseCase_ActivateCard_OtherUser_ReturnsForbidden(t *testing.T) {
	vault := &stubVault{}
	repo := newStubRepo()
	card := newActiveCard("other-user")
	_ = card.Suspend()
	card.ClearEvents()
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, vault)
	_, err := uc.ActivateCard(context.Background(), "user-1", card.ID)
	if !errors.Is(err, model.ErrCardBelongsToOtherUser) {
		t.Errorf("want ErrCardBelongsToOtherUser, got %v", err)
	}
}

func TestCardUseCase_ActivateCard_AlreadyActive_ReturnsError(t *testing.T) {
	vault := &stubVault{}
	repo := newStubRepo()
	card := newActiveCard("user-1") // already ACTIVE
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, vault)
	_, err := uc.ActivateCard(context.Background(), "user-1", card.ID)
	if !errors.Is(err, model.ErrInvalidStateTransition) {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
	if repo.saveCount() != 0 {
		t.Errorf("Save must not be called, got %d", repo.saveCount())
	}
}

// ─────────────────────────────────────────────────────────────────
// GetCard：正常路径 + 归属校验
// ─────────────────────────────────────────────────────────────────

func TestCardUseCase_GetCard_OwnCard_ReturnsCard(t *testing.T) {
	vault := &stubVault{}
	repo := newStubRepo()
	card := newActiveCard("user-1")
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, vault)
	result, err := uc.GetCard(context.Background(), "user-1", card.ID)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if result.ID != card.ID {
		t.Errorf("want ID=%s, got %s", card.ID, result.ID)
	}
	if result.Mask.Last4 != "4242" {
		t.Errorf("want last4=4242, got %s", result.Mask.Last4)
	}
}

func TestCardUseCase_GetCard_OtherUser_ReturnsForbidden(t *testing.T) {
	vault := &stubVault{}
	repo := newStubRepo()
	card := newActiveCard("other-user")
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, vault)
	_, err := uc.GetCard(context.Background(), "user-1", card.ID)
	if !errors.Is(err, model.ErrCardBelongsToOtherUser) {
		t.Errorf("want ErrCardBelongsToOtherUser, got %v", err)
	}
}
