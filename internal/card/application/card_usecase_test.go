package application_test

import (
	"context"
	"errors"
	"testing"

	"payment-demo/internal/card/adapter/vault"
	"payment-demo/internal/card/application"
	"payment-demo/internal/card/domain/model"
	"payment-demo/internal/card/domain/port"
	"payment-demo/internal/card/domain/service"
)

// ─────────────────────────────────────────────────────────────────
// Test Doubles
// ─────────────────────────────────────────────────────────────────

// stubVault 实现 port.CardVault
type stubVault struct {
	cached    map[string]port.CachedCardData
	cacheErr  error
	lastToken string
}

func newStubVault() *stubVault {
	return &stubVault{cached: make(map[string]port.CachedCardData)}
}

func (v *stubVault) CacheTokenizedCard(_ context.Context, data port.CachedCardData) (string, error) {
	if v.cacheErr != nil {
		return "", v.cacheErr
	}
	token := "ct_test_token"
	v.lastToken = token
	v.cached[token] = data
	return token, nil
}

func (v *stubVault) PeekCachedCard(_ context.Context, cardToken, userID string) (*port.CachedCardData, error) {
	data, ok := v.cached[cardToken]
	if !ok {
		return nil, model.ErrCardTokenInvalid
	}
	if data.UserID != userID {
		return nil, model.ErrCardBelongsToOtherUser
	}
	cp := data
	return &cp, nil
}

func (v *stubVault) ConsumeCardToken(_ context.Context, token string) (*port.CachedCardData, error) {
	data, ok := v.cached[token]
	if !ok {
		return nil, model.ErrCardTokenInvalid
	}
	delete(v.cached, token)
	return &data, nil
}

// stubRepo 实现 port.CardRepository
type stubRepo struct {
	byID        map[model.SavedCardID]*model.SavedCard
	byIDErr     map[model.SavedCardID]error
	defaultCard *model.SavedCard
	defaultErr  error
	allCards    []*model.SavedCard
	byPANHash   *model.SavedCard
	byKeyVer    []*model.SavedCard
	saveErr     error
	savedCards  []*model.SavedCard
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
func (r *stubRepo) FindActiveByUserAndPANHash(_ context.Context, _ string, _ model.PANHash) (*model.SavedCard, error) {
	if r.byPANHash != nil {
		return r.byPANHash, nil
	}
	return nil, model.ErrCardNotFound
}
func (r *stubRepo) FindByKeyVersion(_ context.Context, _ int) ([]*model.SavedCard, error) {
	return r.byKeyVer, nil
}
func (r *stubRepo) saveCount() int { return len(r.savedCards) }

// stubEncryption 返回固定加密结果的 EncryptionService
func stubEncryption() *service.EncryptionService {
	km := &stubKeyManager{}
	enc := &stubEncrypter{}
	return service.NewEncryptionService(km, enc)
}

type stubKeyManager struct{}

func (k *stubKeyManager) CurrentDEK() ([]byte, int, error)        { return make([]byte, 32), 1, nil }
func (k *stubKeyManager) DEKByVersion(v int) ([]byte, error)      { return make([]byte, 32), nil }
func (k *stubKeyManager) HMACKey() ([]byte, error)                { return make([]byte, 32), nil }
func (k *stubKeyManager) RotateDEK() (int, error)                 { return 2, nil }
func (k *stubKeyManager) RetireDEK(int) error                     { return nil }
func (k *stubKeyManager) ListVersions() ([]port.KeyVersion, error) { return nil, nil }

type stubEncrypter struct{}

func (e *stubEncrypter) Encrypt(plaintext, dek []byte) ([]byte, error) {
	return append([]byte("enc:"), plaintext...), nil
}
func (e *stubEncrypter) Decrypt(ciphertext, dek []byte) ([]byte, error) {
	if len(ciphertext) > 4 {
		return ciphertext[4:], nil
	}
	return ciphertext, nil
}
func (e *stubEncrypter) HMAC(data, key []byte) (string, error) {
	return "hmac:" + string(data), nil
}

// ─────────────────────────────────────────────────────────────────
// 辅助
// ─────────────────────────────────────────────────────────────────

func buildUseCase(repo port.CardRepository, vault port.CardVault) *application.CardUseCase {
	return application.NewCardUseCase(repo, vault, stubEncryption())
}

func newActiveCard(userID string) *model.SavedCard {
	return model.NewSavedCard(
		userID,
		model.EncryptedPAN{Ciphertext: []byte("enc:4242424242424242"), KeyVersion: 1},
		model.PANHash("hmac:4242424242424242"),
		model.CardMask{Last4: "4242", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
		model.CardHolder{Name: "Alice", BillingCountry: "US"},
	)
}

// ─────────────────────────────────────────────────────────────────
// SuspendCard
// ─────────────────────────────────────────────────────────────────

func TestCardUseCase_SuspendCard_OwnActiveCard_Succeeds(t *testing.T) {
	repo := newStubRepo()
	card := newActiveCard("user-1")
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, newStubVault())
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
}

func TestCardUseCase_SuspendCard_OtherUserCard_ReturnsForbidden(t *testing.T) {
	repo := newStubRepo()
	card := newActiveCard("other-user")
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, newStubVault())
	_, err := uc.SuspendCard(context.Background(), "user-1", card.ID)
	if !errors.Is(err, model.ErrCardBelongsToOtherUser) {
		t.Errorf("want ErrCardBelongsToOtherUser, got %v", err)
	}
}

func TestCardUseCase_SuspendCard_NotFound_ReturnsError(t *testing.T) {
	repo := newStubRepo()
	uc := buildUseCase(repo, newStubVault())
	_, err := uc.SuspendCard(context.Background(), "user-1", "non-existent-id")
	if !errors.Is(err, model.ErrCardNotFound) {
		t.Errorf("want ErrCardNotFound, got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// DeleteCard
// ─────────────────────────────────────────────────────────────────

func TestCardUseCase_DeleteCard_ActiveCard_Succeeds(t *testing.T) {
	repo := newStubRepo()
	card := newActiveCard("user-1")
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, newStubVault())
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
}

func TestCardUseCase_DeleteCard_AlreadyDeleted_ReturnsError(t *testing.T) {
	repo := newStubRepo()
	card := newActiveCard("user-1")
	_ = card.Delete()
	card.ClearEvents()
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, newStubVault())
	err := uc.DeleteCard(context.Background(), "user-1", card.ID)
	if !errors.Is(err, model.ErrInvalidStateTransition) {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// SetDefaultCard
// ─────────────────────────────────────────────────────────────────

func TestCardUseCase_SetDefaultCard_SwitchesDefault(t *testing.T) {
	repo := newStubRepo()

	oldCard := newActiveCard("user-1")
	oldCard.BindAsDefault()
	oldCard.ClearEvents()

	newCard := newActiveCard("user-1")
	repo.byID[oldCard.ID] = oldCard
	repo.byID[newCard.ID] = newCard
	repo.defaultCard = oldCard

	uc := buildUseCase(repo, newStubVault())
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
	if repo.saveCount() != 2 {
		t.Errorf("want Save called 2 times, got %d", repo.saveCount())
	}
}

func TestCardUseCase_SetDefaultCard_SuspendedCard_ReturnsError(t *testing.T) {
	repo := newStubRepo()
	card := newActiveCard("user-1")
	_ = card.Suspend()
	card.ClearEvents()
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, newStubVault())
	_, err := uc.SetDefaultCard(context.Background(), "user-1", card.ID)
	if !errors.Is(err, model.ErrCardNotUsable) {
		t.Errorf("want ErrCardNotUsable, got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// ActivateCard
// ─────────────────────────────────────────────────────────────────

func TestCardUseCase_ActivateCard_FromSuspended_Succeeds(t *testing.T) {
	repo := newStubRepo()
	card := newActiveCard("user-1")
	_ = card.Suspend()
	card.ClearEvents()
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, newStubVault())
	result, err := uc.ActivateCard(context.Background(), "user-1", card.ID)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if result.Status != model.CardStatusActive {
		t.Errorf("want ACTIVE, got %s", result.Status)
	}
}

func TestCardUseCase_ActivateCard_AlreadyActive_ReturnsError(t *testing.T) {
	repo := newStubRepo()
	card := newActiveCard("user-1")
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, newStubVault())
	_, err := uc.ActivateCard(context.Background(), "user-1", card.ID)
	if !errors.Is(err, model.ErrInvalidStateTransition) {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// ListCards / GetCard
// ─────────────────────────────────────────────────────────────────

func TestCardUseCase_ListCards_ReturnsFromRepo(t *testing.T) {
	repo := newStubRepo()
	repo.allCards = []*model.SavedCard{newActiveCard("user-1"), newActiveCard("user-1")}

	uc := buildUseCase(repo, newStubVault())
	cards, err := uc.ListCards(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if len(cards) != 2 {
		t.Errorf("want 2 cards, got %d", len(cards))
	}
}

func TestCardUseCase_GetCard_OwnCard_ReturnsCard(t *testing.T) {
	repo := newStubRepo()
	card := newActiveCard("user-1")
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, newStubVault())
	result, err := uc.GetCard(context.Background(), "user-1", card.ID)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if result.ID != card.ID {
		t.Errorf("want ID=%s, got %s", card.ID, result.ID)
	}
}

func TestCardUseCase_GetCard_OtherUser_ReturnsForbidden(t *testing.T) {
	repo := newStubRepo()
	card := newActiveCard("other-user")
	repo.byID[card.ID] = card

	uc := buildUseCase(repo, newStubVault())
	_, err := uc.GetCard(context.Background(), "user-1", card.ID)
	if !errors.Is(err, model.ErrCardBelongsToOtherUser) {
		t.Errorf("want ErrCardBelongsToOtherUser, got %v", err)
	}
}

func TestCardUseCase_ResolveCardForGateway_AfterTokenize_CreatesSeparateGatewayToken(t *testing.T) {
	ctx := context.Background()
	repo := newStubRepo()
	v := vault.NewLocalVault()
	uc := application.NewCardUseCase(repo, v, stubEncryption())

	tokRes, err := uc.Tokenize(ctx, application.TokenizeRequest{
		UserID:         "u1",
		PAN:            "4242424242424242",
		ExpiryMonth:    12,
		ExpiryYear:     2030,
		CVV:            "123",
		CardholderName: "Test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tokRes.CardToken == nil {
		t.Fatal("want CardToken from tokenize")
	}
	orig := *tokRes.CardToken

	gw, err := uc.ResolveCardForGateway(ctx, orig, "u1")
	if err != nil {
		t.Fatalf("ResolveCardForGateway: %v", err)
	}
	if gw.GatewayToken == orig {
		t.Fatal("gateway token should differ from original ct_")
	}
	if gw.Last4 != "4242" {
		t.Errorf("Last4: want 4242, got %q", gw.Last4)
	}
	peekOrig, err := v.PeekCachedCard(ctx, orig, "u1")
	if err != nil || len(peekOrig.EncryptedPAN.Ciphertext) == 0 {
		t.Fatalf("original ct_ should remain with ciphertext: err=%v", err)
	}
	gwPeek, err := v.PeekCachedCard(ctx, gw.GatewayToken, "u1")
	if err != nil || gwPeek.RawPAN != "4242424242424242" {
		t.Fatalf("gateway ct_ should hold RawPAN: err=%v pan=%q", err, gwPeek.RawPAN)
	}
}
