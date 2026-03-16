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
	cardHTTP "payment-demo/internal/card/handler/http"
	identityMW "payment-demo/internal/identity/handler/middleware"
)

// ─────────────────────────────────────────────────────────────────
// Vault stub
// ─────────────────────────────────────────────────────────────────

type testVault struct {
	result    *port.VaultResult
	tokenErr  error
	deleteErr error
}

func (v *testVault) Tokenize(_ context.Context, _ string) (*port.VaultResult, error) {
	return v.result, v.tokenErr
}

func (v *testVault) Delete(_ context.Context, _ model.VaultToken) error {
	return v.deleteErr
}

func successVault() *testVault {
	return &testVault{
		result: &port.VaultResult{
			VaultToken: model.VaultToken{Token: "vault_tok_handler", Provider: "mock"},
			Mask:       model.CardMask{Last4: "4242", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
			Holder:     model.CardHolder{Name: "Test User", BillingCountry: "US"},
		},
	}
}

func failVault() *testVault {
	return &testVault{tokenErr: model.ErrVaultTokenizeFailed}
}

// ─────────────────────────────────────────────────────────────────
// W-8: Repository stub（实现 port.CardRepository 接口，替代具体 persistence 实现）
// ─────────────────────────────────────────────────────────────────

// testRepo 是仅用于 handler 测试的内存仓储 stub，通过接口解耦具体实现。
type testRepo struct {
	data map[model.SavedCardID]*model.SavedCard
}

func newTestRepo() *testRepo {
	return &testRepo{data: make(map[model.SavedCardID]*model.SavedCard)}
}

// seed 预存一张卡，供测试设置初始状态
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

// 编译期验证 testRepo 实现了 port.CardRepository
var _ port.CardRepository = (*testRepo)(nil)

// ─────────────────────────────────────────────────────────────────
// 测试基础设施
// ─────────────────────────────────────────────────────────────────

// setup 构建一个带有 userID 注入 middleware 的 http.Handler
// W-8: 接受 port.CardRepository 接口，不再耦合具体 persistence 实现
// R-1: 使用 card/adapter/auth.WithUserID，消除对 identity 包的直接依赖
func setup(repo port.CardRepository, vault port.CardVault, userID string) http.Handler {
	uc := cardApp.NewCardUseCase(repo, vault)
	h := cardHTTP.NewCardHandler(uc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// 用 auth.WithUserID 注入 userID，保证与 handler 中 UserIDFromContext 的 key 一致
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := identityMW.WithUserID(r.Context(), userID)
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

// setupNoAuth 构建不注入 userID 的 handler（用于测试 401 路径）
func setupNoAuth(repo port.CardRepository, vault port.CardVault) http.Handler {
	uc := cardApp.NewCardUseCase(repo, vault)
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

// ─────────────────────────────────────────────────────────────────
// AC-35  POST /cards 绑卡成功 → 201 + card_id
// ─────────────────────────────────────────────────────────────────

func TestCardHandler_BindCard_Success_Returns201(t *testing.T) {
	repo := newTestRepo()
	handler := setup(repo, successVault(), "user-1")

	w := doRequest(handler, http.MethodPost, "/cards", jsonBody(map[string]string{
		"one_time_token": "tok_frontend_xyz",
	}))

	if w.Code != http.StatusCreated {
		t.Errorf("want 201, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// AC-35 要求响应含 card_id 字段
	if resp["card_id"] == "" || resp["card_id"] == nil {
		t.Errorf("want non-empty 'card_id' in response, got: %v", resp)
	}
	// 也检查 id 字段（两者相同）
	if resp["id"] == "" || resp["id"] == nil {
		t.Errorf("want non-empty 'id' in response, got: %v", resp)
	}
}

// AC-36  POST /cards 无 userID（无 Authorization）→ 401
func TestCardHandler_BindCard_NoAuth_Returns401(t *testing.T) {
	handler := setupNoAuth(newTestRepo(), successVault())

	w := doRequest(handler, http.MethodPost, "/cards", jsonBody(map[string]string{
		"one_time_token": "tok_frontend_xyz",
	}))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] == "" {
		t.Error("want 'error' field in response body")
	}
}

// POST /cards body 为空 → 400
func TestCardHandler_BindCard_EmptyToken_Returns400(t *testing.T) {
	handler := setup(newTestRepo(), successVault(), "user-1")

	w := doRequest(handler, http.MethodPost, "/cards", jsonBody(map[string]string{
		"one_time_token": "",
	}))

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// POST /cards Vault 失败 → 502
func TestCardHandler_BindCard_VaultFails_Returns502(t *testing.T) {
	handler := setup(newTestRepo(), failVault(), "user-1")

	w := doRequest(handler, http.MethodPost, "/cards", jsonBody(map[string]string{
		"one_time_token": "tok_fail",
	}))

	if w.Code != http.StatusBadGateway {
		t.Errorf("want 502, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-37  POST /cards/suspend 挂起成功 → 200 + status=SUSPENDED
// ─────────────────────────────────────────────────────────────────

func TestCardHandler_SuspendCard_Success_Returns200(t *testing.T) {
	repo := newTestRepo()
	// 预存一张 Active 卡
	card := model.NewSavedCard("user-1",
		model.VaultToken{Token: "tok_1", Provider: "mock"},
		model.CardMask{Last4: "4242", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
		model.CardHolder{Name: "Alice", BillingCountry: "US"},
	)
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

// AC-38  POST /cards/suspend 操作他人卡 → 403
func TestCardHandler_SuspendCard_OtherUserCard_Returns403(t *testing.T) {
	repo := newTestRepo()
	// 卡属于 user-2
	card := model.NewSavedCard("user-2",
		model.VaultToken{Token: "tok_2", Provider: "mock"},
		model.CardMask{Last4: "9999", Brand: "Mastercard", ExpireMonth: 6, ExpireYear: 2026},
		model.CardHolder{Name: "Bob", BillingCountry: "UK"},
	)
	repo.seed(card)

	// 以 user-1 身份发请求
	handler := setup(repo, successVault(), "user-1")
	w := doRequest(handler, http.MethodPost, "/cards/suspend", jsonBody(map[string]string{
		"card_id": string(card.ID),
	}))

	if w.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] == "" {
		t.Error("want 'error' field in response")
	}
}

// AC-42  POST /cards/suspend 卡不存在 → 404
func TestCardHandler_SuspendCard_NotFound_Returns404(t *testing.T) {
	repo := newTestRepo()
	handler := setup(repo, successVault(), "user-1")

	w := doRequest(handler, http.MethodPost, "/cards/suspend", jsonBody(map[string]string{
		"card_id": "card-999",
	}))

	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] == "" {
		t.Error("want 'error' field in 404 response")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-39  DELETE /cards 删除成功 → 200 + {"status":"DELETED"}
// ─────────────────────────────────────────────────────────────────

func TestCardHandler_DeleteCard_Success_Returns200WithStatus(t *testing.T) {
	repo := newTestRepo()
	card := model.NewSavedCard("user-1",
		model.VaultToken{Token: "tok_del", Provider: "mock"},
		model.CardMask{Last4: "4242", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
		model.CardHolder{Name: "Alice", BillingCountry: "US"},
	)
	repo.seed(card)

	handler := setup(repo, successVault(), "user-1")
	w := doRequest(handler, http.MethodDelete, "/cards", jsonBody(map[string]string{
		"card_id": string(card.ID),
	}))

	// AC-39 要求 200 OK
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	// AC-39 要求响应 body 包含 "status": "DELETED"
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != string(model.CardStatusDeleted) {
		t.Errorf("want status=DELETED in body, got: %v", resp["status"])
	}
}

// DELETE /cards 无 userID → 401
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
// AC-40  GET /cards 返回未删除的卡列表
// ─────────────────────────────────────────────────────────────────

func TestCardHandler_ListCards_ReturnsNonDeletedCards(t *testing.T) {
	repo := newTestRepo()

	// 存 2 张 Active 卡 + 1 张 Deleted 卡
	active1 := model.NewSavedCard("user-1",
		model.VaultToken{Token: "tok_a1", Provider: "mock"},
		model.CardMask{Last4: "4242", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
		model.CardHolder{Name: "Alice", BillingCountry: "US"},
	)
	active2 := model.NewSavedCard("user-1",
		model.VaultToken{Token: "tok_a2", Provider: "mock"},
		model.CardMask{Last4: "5353", Brand: "Mastercard", ExpireMonth: 8, ExpireYear: 2027},
		model.CardHolder{Name: "Alice", BillingCountry: "US"},
	)
	deleted := model.NewSavedCard("user-1",
		model.VaultToken{Token: "tok_del", Provider: "mock"},
		model.CardMask{Last4: "0000", Brand: "UnionPay", ExpireMonth: 1, ExpireYear: 2025},
		model.CardHolder{Name: "Alice", BillingCountry: "CN"},
	)
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
	// AC-40 要求 Deleted 卡不返回 → 只返回 2 张
	if len(cards) != 2 {
		t.Errorf("want 2 cards in list, got %d", len(cards))
	}
	// 每张卡应包含必要字段（AC-40）
	for _, c := range cards {
		if c["id"] == nil || c["id"] == "" {
			t.Error("want non-empty 'id' in card item")
		}
		if c["last4"] == nil {
			t.Error("want 'last4' in card item")
		}
		if c["is_default"] == nil {
			t.Error("want 'is_default' in card item")
		}
		if c["status"] == nil {
			t.Error("want 'status' in card item")
		}
	}
}

// GET /cards 无 userID → 401
func TestCardHandler_ListCards_NoAuth_Returns401(t *testing.T) {
	handler := setupNoAuth(newTestRepo(), successVault())
	w := doRequest(handler, http.MethodGet, "/cards", nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-41  PUT /cards/default 切换默认卡 → 200
//
//	（POST /cards/default 同样被 handler 接受）
//
// ─────────────────────────────────────────────────────────────────
func TestCardHandler_SetDefault_SwitchesDefault_Returns200(t *testing.T) {
	repo := newTestRepo()

	oldDefault := model.NewSavedCard("user-1",
		model.VaultToken{Token: "tok_old", Provider: "mock"},
		model.CardMask{Last4: "1111", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
		model.CardHolder{Name: "Alice", BillingCountry: "US"},
	)
	oldDefault.BindAsDefault()
	oldDefault.ClearEvents()
	repo.seed(oldDefault)

	newCard := model.NewSavedCard("user-1",
		model.VaultToken{Token: "tok_new", Provider: "mock"},
		model.CardMask{Last4: "2222", Brand: "Mastercard", ExpireMonth: 6, ExpireYear: 2030},
		model.CardHolder{Name: "Alice", BillingCountry: "US"},
	)
	repo.seed(newCard)

	handler := setup(repo, successVault(), "user-1")

	// AC-41 使用 PUT 方法
	w := doRequest(handler, http.MethodPut, "/cards/default", jsonBody(map[string]string{
		"card_id": string(newCard.ID),
	}))

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	// AC-41 验证：通过 repo 直接读取确认 IsDefault 已切换
	updatedNew, _ := repo.FindByID(context.Background(), newCard.ID)
	if !updatedNew.IsDefault {
		t.Error("new card should be default after PUT /default")
	}
	updatedOld, _ := repo.FindByID(context.Background(), oldDefault.ID)
	if updatedOld.IsDefault {
		t.Error("old default card should no longer be default")
	}
}

// POST /cards/default 对 Suspended 卡 → 422
func TestCardHandler_SetDefault_SuspendedCard_Returns422(t *testing.T) {
	repo := newTestRepo()

	suspendedCard := model.NewSavedCard("user-1",
		model.VaultToken{Token: "tok_sus", Provider: "mock"},
		model.CardMask{Last4: "3333", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
		model.CardHolder{Name: "Alice", BillingCountry: "US"},
	)
	_ = suspendedCard.Suspend()
	suspendedCard.ClearEvents()
	repo.seed(suspendedCard)

	handler := setup(repo, successVault(), "user-1")
	w := doRequest(handler, http.MethodPost, "/cards/default", jsonBody(map[string]string{
		"card_id": string(suspendedCard.ID),
	}))

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────
// POST /cards/activate — 激活挂起卡
// ─────────────────────────────────────────────────────────────────

func TestCardHandler_ActivateCard_FromSuspended_Returns200(t *testing.T) {
	repo := newTestRepo()

	card := model.NewSavedCard("user-1",
		model.VaultToken{Token: "tok_act", Provider: "mock"},
		model.CardMask{Last4: "4444", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
		model.CardHolder{Name: "Alice", BillingCountry: "US"},
	)
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
// GET /cards?id=xxx — 查询单张卡详情
// ─────────────────────────────────────────────────────────────────

func TestCardHandler_GetCard_Success_Returns200(t *testing.T) {
	repo := newTestRepo()

	card := model.NewSavedCard("user-1",
		model.VaultToken{Token: "tok_get", Provider: "mock"},
		model.CardMask{Last4: "5555", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
		model.CardHolder{Name: "Alice", BillingCountry: "US"},
	)
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
	if resp["last4"] != card.Mask.Last4 {
		t.Errorf("want last4=%s, got %v", card.Mask.Last4, resp["last4"])
	}
}

// GET /cards?id=xxx 卡不存在 → 404
func TestCardHandler_GetCard_NotFound_Returns404(t *testing.T) {
	handler := setup(newTestRepo(), successVault(), "user-1")
	w := doRequest(handler, http.MethodGet, "/cards?id=card-nonexistent", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// POST /cards/suspend 挂起已挂起的卡 → 409 Conflict
// ─────────────────────────────────────────────────────────────────

func TestCardHandler_SuspendCard_AlreadySuspended_Returns409(t *testing.T) {
	repo := newTestRepo()

	card := model.NewSavedCard("user-1",
		model.VaultToken{Token: "tok_sus", Provider: "mock"},
		model.CardMask{Last4: "6666", Brand: "Visa", ExpireMonth: 12, ExpireYear: 2028},
		model.CardHolder{Name: "Alice", BillingCountry: "US"},
	)
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
// 方法不允许 → 405
// ─────────────────────────────────────────────────────────────────

func TestCardHandler_MethodNotAllowed_OnCards(t *testing.T) {
	handler := setup(newTestRepo(), successVault(), "user-1")
	w := doRequest(handler, http.MethodPatch, "/cards", nil)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", w.Code)
	}
}
