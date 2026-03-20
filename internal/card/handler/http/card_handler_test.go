package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cardApp "payment-demo/internal/card/application"
	"payment-demo/internal/card/domain/model"
	"payment-demo/internal/card/domain/port"
	"payment-demo/internal/card/domain/service"
	cardHTTP "payment-demo/internal/card/handler/http"
	"payment-demo/internal/shared/auth"
)

// ─────────────────────────────────────────────────────────────────
// Vault stub
// ─────────────────────────────────────────────────────────────────

type testVault struct {
	cached   map[string]port.CachedCardData
	cacheErr error
}

func (v *testVault) CacheTokenizedCard(_ context.Context, data port.CachedCardData) (string, error) {
	if v.cacheErr != nil {
		return "", v.cacheErr
	}
	token := "ct_test_token"
	v.cached[token] = data
	return token, nil
}

func (v *testVault) PeekCachedCard(_ context.Context, cardToken, userID string) (*port.CachedCardData, error) {
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

func (v *testVault) ConsumeCardToken(_ context.Context, token string) (*port.CachedCardData, error) {
	data, ok := v.cached[token]
	if !ok {
		return nil, model.ErrCardTokenInvalid
	}
	delete(v.cached, token)
	return &data, nil
}

func successVault() *testVault {
	v := &testVault{cached: make(map[string]port.CachedCardData)}
	// Pre-seed a ct_test_token for bind tests
	v.cached["ct_test_token"] = port.CachedCardData{
		EncryptedPAN: model.EncryptedPAN{Ciphertext: []byte("enc:4242424242424242"), KeyVersion: 1},
		PANHash:      model.PANHash("hmac:4242"),
		Mask:         model.CardMask{Last4: "4242", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
		Holder:       model.CardHolder{Name: "Test User", BillingCountry: "US"},
		UserID:       "user-1",
	}
	return v
}

// ─────────────────────────────────────────────────────────────────
// Repository stub
// ─────────────────────────────────────────────────────────────────

type testRepo struct {
	data map[model.SavedCardID]*model.SavedCard
}

func newTestRepo() *testRepo {
	return &testRepo{data: make(map[model.SavedCardID]*model.SavedCard)}
}

func (r *testRepo) seed(card *model.SavedCard) {
	c := *card
	r.data[card.ID] = &c
}

func (r *testRepo) Save(_ context.Context, card *model.SavedCard) error {
	c := *card
	r.data[card.ID] = &c
	return nil
}

func (r *testRepo) FindByID(_ context.Context, id model.SavedCardID) (*model.SavedCard, error) {
	card, ok := r.data[id]
	if !ok {
		return nil, model.ErrCardNotFound
	}
	c := *card
	return &c, nil
}

func (r *testRepo) FindAllByUserID(_ context.Context, userID string) ([]*model.SavedCard, error) {
	var result []*model.SavedCard
	for _, card := range r.data {
		if card.UserID == userID && card.Status != model.CardStatusDeleted {
			c := *card
			result = append(result, &c)
		}
	}
	return result, nil
}

func (r *testRepo) FindDefaultByUserID(_ context.Context, userID string) (*model.SavedCard, error) {
	for _, card := range r.data {
		if card.UserID == userID && card.IsDefault && card.Status != model.CardStatusDeleted {
			c := *card
			return &c, nil
		}
	}
	return nil, nil
}

func (r *testRepo) FindActiveByUserAndPANHash(_ context.Context, _ string, _ model.PANHash) (*model.SavedCard, error) {
	return nil, model.ErrCardNotFound
}

func (r *testRepo) FindByKeyVersion(_ context.Context, _ int) ([]*model.SavedCard, error) {
	return nil, nil
}

var _ port.CardRepository = (*testRepo)(nil)

// ─────────────────────────────────────────────────────────────────
// Encryption stubs
// ─────────────────────────────────────────────────────────────────

type stubKeyManager struct{}

func (k *stubKeyManager) CurrentDEK() ([]byte, int, error)         { return make([]byte, 32), 1, nil }
func (k *stubKeyManager) DEKByVersion(int) ([]byte, error)         { return make([]byte, 32), nil }
func (k *stubKeyManager) HMACKey() ([]byte, error)                 { return make([]byte, 32), nil }
func (k *stubKeyManager) RotateDEK() (int, error)                  { return 2, nil }
func (k *stubKeyManager) RetireDEK(int) error                      { return nil }
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

func stubEncryption() *service.EncryptionService {
	return service.NewEncryptionService(&stubKeyManager{}, &stubEncrypter{})
}

// ─────────────────────────────────────────────────────────────────
// 测试基础设施
// ─────────────────────────────────────────────────────────────────

func setup(repo port.CardRepository, vault port.CardVault, userID string) http.Handler {
	uc := cardApp.NewCardUseCase(repo, vault, stubEncryption())
	h := cardHTTP.NewCardHandler(uc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.WithUserID(r.Context(), userID)
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

func setupNoAuth(repo port.CardRepository, vault port.CardVault) http.Handler {
	uc := cardApp.NewCardUseCase(repo, vault, stubEncryption())
	h := cardHTTP.NewCardHandler(uc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux
}

func doRequest(handler http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	var buf *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	} else {
		buf = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func jsonBody(v interface{}) interface{} { return v }

func newTestSavedCard(userID string) *model.SavedCard {
	return model.NewSavedCard(
		userID,
		model.EncryptedPAN{Ciphertext: []byte("enc:4242424242424242"), KeyVersion: 1},
		model.PANHash("hmac:4242"),
		model.CardMask{Last4: "4242", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
		model.CardHolder{Name: "Alice", BillingCountry: "US"},
	)
}

// ─────────────────────────────────────────────────────────────────
// POST /cards → 405（绑卡只在支付成功后内部触发，不暴露 HTTP 端点）
// ─────────────────────────────────────────────────────────────────

func TestCardHandler_PostCards_Returns405(t *testing.T) {
	handler := setup(newTestRepo(), successVault(), "user-1")
	w := doRequest(handler, http.MethodPost, "/cards", jsonBody(map[string]string{
		"one_time_token": "ct_test_token",
	}))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────
// POST /cards/suspend
// ─────────────────────────────────────────────────────────────────

func TestCardHandler_SuspendCard_Success_Returns200(t *testing.T) {
	repo := newTestRepo()
	card := newTestSavedCard("user-1")
	repo.seed(card)

	handler := setup(repo, successVault(), "user-1")
	w := doRequest(handler, http.MethodPost, "/cards/suspend", jsonBody(map[string]string{
		"card_id": string(card.ID),
	}))

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != string(model.CardStatusSuspended) {
		t.Errorf("want status=SUSPENDED, got %v", resp["status"])
	}
}

func TestCardHandler_SuspendCard_OtherUserCard_Returns403(t *testing.T) {
	repo := newTestRepo()
	card := newTestSavedCard("user-2")
	repo.seed(card)

	handler := setup(repo, successVault(), "user-1")
	w := doRequest(handler, http.MethodPost, "/cards/suspend", jsonBody(map[string]string{
		"card_id": string(card.ID),
	}))

	if w.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestCardHandler_SuspendCard_NotFound_Returns404(t *testing.T) {
	handler := setup(newTestRepo(), successVault(), "user-1")

	w := doRequest(handler, http.MethodPost, "/cards/suspend", jsonBody(map[string]string{
		"card_id": "card-999",
	}))

	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestCardHandler_SuspendCard_AlreadySuspended_Returns409(t *testing.T) {
	repo := newTestRepo()
	card := newTestSavedCard("user-1")
	_ = card.Suspend()
	card.ClearEvents()
	repo.seed(card)

	handler := setup(repo, successVault(), "user-1")
	w := doRequest(handler, http.MethodPost, "/cards/suspend", jsonBody(map[string]string{
		"card_id": string(card.ID),
	}))

	if w.Code != http.StatusConflict {
		t.Errorf("want 409, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────
// DELETE /cards
// ─────────────────────────────────────────────────────────────────

func TestCardHandler_DeleteCard_Success_Returns200WithStatus(t *testing.T) {
	repo := newTestRepo()
	card := newTestSavedCard("user-1")
	repo.seed(card)

	handler := setup(repo, successVault(), "user-1")
	w := doRequest(handler, http.MethodDelete, "/cards", jsonBody(map[string]string{
		"card_id": string(card.ID),
	}))

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != string(model.CardStatusDeleted) {
		t.Errorf("want status=DELETED in body, got: %v", resp["status"])
	}
}

func TestCardHandler_DeleteCard_NoAuth_Returns401(t *testing.T) {
	handler := setupNoAuth(newTestRepo(), successVault())
	w := doRequest(handler, http.MethodDelete, "/cards", jsonBody(map[string]string{
		"card_id": "card-1",
	}))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// GET /cards
// ─────────────────────────────────────────────────────────────────

func TestCardHandler_ListCards_ReturnsNonDeletedCards(t *testing.T) {
	repo := newTestRepo()

	active1 := newTestSavedCard("user-1")
	active2 := newTestSavedCard("user-1")
	deleted := newTestSavedCard("user-1")
	_ = deleted.Delete()
	deleted.ClearEvents()

	repo.seed(active1)
	repo.seed(active2)
	repo.seed(deleted)

	handler := setup(repo, successVault(), "user-1")
	w := doRequest(handler, http.MethodGet, "/cards", nil)

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var cards []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&cards); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(cards) != 2 {
		t.Errorf("want 2 cards in list, got %d", len(cards))
	}
}

func TestCardHandler_ListCards_NoAuth_Returns401(t *testing.T) {
	handler := setupNoAuth(newTestRepo(), successVault())
	w := doRequest(handler, http.MethodGet, "/cards", nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// PUT /cards/default
// ─────────────────────────────────────────────────────────────────

func TestCardHandler_SetDefault_SwitchesDefault_Returns200(t *testing.T) {
	repo := newTestRepo()

	oldDefault := newTestSavedCard("user-1")
	oldDefault.BindAsDefault()
	oldDefault.ClearEvents()
	repo.seed(oldDefault)

	newCard := newTestSavedCard("user-1")
	repo.seed(newCard)

	handler := setup(repo, successVault(), "user-1")
	w := doRequest(handler, http.MethodPut, "/cards/default", jsonBody(map[string]string{
		"card_id": string(newCard.ID),
	}))

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	updatedNew, _ := repo.FindByID(context.Background(), newCard.ID)
	if !updatedNew.IsDefault {
		t.Error("new card should be default after PUT /default")
	}
	updatedOld, _ := repo.FindByID(context.Background(), oldDefault.ID)
	if updatedOld.IsDefault {
		t.Error("old default card should no longer be default")
	}
}

func TestCardHandler_SetDefault_SuspendedCard_Returns422(t *testing.T) {
	repo := newTestRepo()
	card := newTestSavedCard("user-1")
	_ = card.Suspend()
	card.ClearEvents()
	repo.seed(card)

	handler := setup(repo, successVault(), "user-1")
	w := doRequest(handler, http.MethodPost, "/cards/default", jsonBody(map[string]string{
		"card_id": string(card.ID),
	}))

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────
// POST /cards/activate
// ─────────────────────────────────────────────────────────────────

func TestCardHandler_ActivateCard_FromSuspended_Returns200(t *testing.T) {
	repo := newTestRepo()
	card := newTestSavedCard("user-1")
	_ = card.Suspend()
	card.ClearEvents()
	repo.seed(card)

	handler := setup(repo, successVault(), "user-1")
	w := doRequest(handler, http.MethodPost, "/cards/activate", jsonBody(map[string]string{
		"card_id": string(card.ID),
	}))

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != string(model.CardStatusActive) {
		t.Errorf("want status=ACTIVE, got %v", resp["status"])
	}
}

// ─────────────────────────────────────────────────────────────────
// GET /cards?id=xxx
// ─────────────────────────────────────────────────────────────────

func TestCardHandler_GetCard_Success_Returns200(t *testing.T) {
	repo := newTestRepo()
	card := newTestSavedCard("user-1")
	repo.seed(card)

	handler := setup(repo, successVault(), "user-1")
	w := doRequest(handler, http.MethodGet, "/cards?id="+string(card.ID), nil)

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["id"] != string(card.ID) {
		t.Errorf("want id=%s, got %v", card.ID, resp["id"])
	}
}

func TestCardHandler_GetCard_NotFound_Returns404(t *testing.T) {
	handler := setup(newTestRepo(), successVault(), "user-1")
	w := doRequest(handler, http.MethodGet, "/cards?id=card-nonexistent", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// Method not allowed
// ─────────────────────────────────────────────────────────────────

func TestCardHandler_MethodNotAllowed_OnCards(t *testing.T) {
	handler := setup(newTestRepo(), successVault(), "user-1")
	for _, method := range []string{http.MethodPatch, http.MethodPut} {
		w := doRequest(handler, method, "/cards", nil)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /cards: want 405, got %d", method, w.Code)
		}
	}
}
