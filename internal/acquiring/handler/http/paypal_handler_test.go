package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"payment-demo/internal/acquiring/adapter/persistence"
	"payment-demo/internal/acquiring/application"
	paymentModel "payment-demo/internal/acquiring/domain/model"
	"payment-demo/internal/acquiring/domain/port"
	paymentHTTP "payment-demo/internal/acquiring/handler/http"
	"payment-demo/internal/shared/auth"
)

// ─────────────────────────────────────────────────────────────────
// Test Doubles（Handler 测试专用）
// ─────────────────────────────────────────────────────────────────

type handlerStubCardGateway struct {
	authorizeResult *port.GatewayAuthResult
	authorizeErr    error
}

func (g *handlerStubCardGateway) Authorize(_ context.Context, _ paymentModel.CardToken, _ paymentModel.Money) (*port.GatewayAuthResult, error) {
	return g.authorizeResult, g.authorizeErr
}
func (g *handlerStubCardGateway) Capture(_ context.Context, _ string, _ paymentModel.Money) error {
	return nil
}
func (g *handlerStubCardGateway) Refund(_ context.Context, _ string, _ paymentModel.Money) error {
	return nil
}

type handlerStubPayPalGateway struct {
	authorizeResult *port.PayPalAuthResult
	authorizeErr    error
	captureErr      error
	refundErr       error
}

func (g *handlerStubPayPalGateway) Authorize(_ context.Context, _ paymentModel.PayPalToken, _ paymentModel.Money) (*port.PayPalAuthResult, error) {
	return g.authorizeResult, g.authorizeErr
}
func (g *handlerStubPayPalGateway) Capture(_ context.Context, _ string, _ paymentModel.Money) error {
	return g.captureErr
}
func (g *handlerStubPayPalGateway) Refund(_ context.Context, _ string, _ paymentModel.Money) error {
	return g.refundErr
}

type handlerStubCardQuery struct{}

func (q *handlerStubCardQuery) FindActiveCard(_ context.Context, _ string) (*port.SavedCardView, error) {
	return nil, paymentModel.ErrCardNotFound
}

type handlerStubCardCommand struct{}

func (c *handlerStubCardCommand) StoreChannelToken(_ context.Context, _, _, _, _ string) error {
	return nil
}
func (c *handlerStubCardCommand) BindCardFromToken(_ context.Context, _ port.BindFromTokenCommand) (string, error) {
	return "card-new", nil
}
func (c *handlerStubCardCommand) PrepareOneTimeToken(_ context.Context, _, _ string) (string, error) {
	return "ct_onetime", nil
}
func (c *handlerStubCardCommand) ResolveCardForGateway(_ context.Context, _, _ string) (*port.ResolvedCard, error) {
	return &port.ResolvedCard{Last4: "4242", Brand: "visa", GatewayToken: "ct_gw_stub"}, nil
}

// ─────────────────────────────────────────────────────────────────
// 多商户桩
// ─────────────────────────────────────────────────────────────────

type handlerMerchantRepo struct {
	merchant *paymentModel.Merchant
	err      error
}

func (r *handlerMerchantRepo) Save(_ context.Context, _ *paymentModel.Merchant) error { return nil }
func (r *handlerMerchantRepo) FindByID(_ context.Context, _ paymentModel.MerchantID) (*paymentModel.Merchant, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.merchant, nil
}
func (r *handlerMerchantRepo) FindAll(_ context.Context) ([]*paymentModel.Merchant, error) {
	return nil, nil
}

type handlerGatewayFactory struct {
	cardGw   port.PaymentGateway
	paypalGw port.PayPalGateway
}

func (f *handlerGatewayFactory) BuildCardGateway(_ port.ChannelCredentialView) (port.PaymentGateway, error) {
	return f.cardGw, nil
}
func (f *handlerGatewayFactory) BuildPayPalGateway(_ port.ChannelCredentialView) (port.PayPalGateway, error) {
	return f.paypalGw, nil
}

func defaultMerchantEntity() *paymentModel.Merchant {
	return &paymentModel.Merchant{
		ID:     paymentModel.MerchantID("merchant-1"),
		Status: paymentModel.MerchantStatusActive,
		Credentials: []paymentModel.ChannelCredential{
			{
				ID:      paymentModel.ChannelCredentialID("cred-1"),
				Channel: paymentModel.PaymentMethodCard,
				Secrets: map[string]string{"api_key": "sk_test_xxx"},
				Status:  paymentModel.CredentialStatusActive,
			},
			{
				ID:      paymentModel.ChannelCredentialID("cred-paypal-1"),
				Channel: paymentModel.PaymentMethodPayPal,
				Secrets: map[string]string{"client_id": "cl_xxx", "client_secret": "sec_xxx"},
				Status:  paymentModel.CredentialStatusActive,
			},
		},
	}
}

// ─────────────────────────────────────────────────────────────────
// 辅助：构建可测试的 HTTP handler + mux
// ─────────────────────────────────────────────────────────────────

type handlerTestSetup struct {
	mux  *http.ServeMux
	repo *persistence.InMemoryTransactionRepository
	uc   *application.ChargeUseCase
}

func buildHandlerSetup(webhookSecret string) *handlerTestSetup {
	repo := persistence.NewInMemoryTransactionRepository()
	cardGw := &handlerStubCardGateway{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "pi_card_001", AuthCode: "AUTH"},
	}
	paypalGw := &handlerStubPayPalGateway{
		authorizeResult: &port.PayPalAuthResult{ProviderRef: "CAPTURE-001"},
	}
	merchantRepo := &handlerMerchantRepo{merchant: defaultMerchantEntity()}
	factory := &handlerGatewayFactory{cardGw: cardGw, paypalGw: paypalGw}

	uc := application.NewChargeUseCase(merchantRepo, factory, repo, &handlerStubCardQuery{}, &handlerStubCardCommand{})
	handler := paymentHTTP.NewPaymentHandler(uc, webhookSecret)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	return &handlerTestSetup{mux: mux, repo: repo, uc: uc}
}

func withAuth(r *http.Request, userID string) *http.Request {
	return r.WithContext(auth.WithUserID(r.Context(), userID))
}

func doRequest(mux *http.ServeMux, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func parseJSON(t *testing.T, body string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("parseJSON: %v (body=%q)", err, body)
	}
	return m
}

// ─────────────────────────────────────────────────────────────────
// GET /internal/transaction — 查询成功返回 200
// ─────────────────────────────────────────────────────────────────

func TestPaymentHandler_GetInternalTransaction_Success(t *testing.T) {
	setup := buildHandlerSetup("")

	txn, err := setup.uc.Purchase(context.Background(), application.PurchaseRequest{
		MerchantID: "merchant-1",
		UserID:     "user-alice",
		OrderID:    "order-001",
		Amount:     paymentModel.NewMoney(1000, "USD"),
		Token:      paymentModel.CardToken{TokenID: "tok_visa", Last4: "4242", Brand: "Visa"},
	})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/internal/transaction?id="+string(txn.ID), nil)
	req = withAuth(req, "user-alice")
	w := doRequest(setup.mux, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.String())
	if resp["id"] != string(txn.ID) {
		t.Errorf("id: want %s, got %v", txn.ID, resp["id"])
	}
	if resp["status"] != "AUTHORIZED" {
		t.Errorf("status: want AUTHORIZED, got %v", resp["status"])
	}
	if resp["order_id"] != "order-001" {
		t.Errorf("order_id: want order-001, got %v", resp["order_id"])
	}
	if resp["method"] != "CARD" {
		t.Errorf("method: want CARD, got %v", resp["method"])
	}
	if resp["merchant_id"] != "merchant-1" {
		t.Errorf("merchant_id: want merchant-1, got %v", resp["merchant_id"])
	}
}

// ─────────────────────────────────────────────────────────────────
// GET /internal/transaction — 无认证返回 401
// ─────────────────────────────────────────────────────────────────

func TestPaymentHandler_GetInternalTransaction_NoAuth_Returns401(t *testing.T) {
	setup := buildHandlerSetup("")

	req := httptest.NewRequest(http.MethodGet, "/internal/transaction?id=any-id", nil)
	w := doRequest(setup.mux, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status: want 401, got %d", w.Code)
	}
	resp := parseJSON(t, w.Body.String())
	if resp["error"] == nil {
		t.Error("response must contain 'error' field")
	}
}

// ─────────────────────────────────────────────────────────────────
// GET /internal/transaction — 缺少 id 参数返回 400
// ─────────────────────────────────────────────────────────────────

func TestPaymentHandler_GetInternalTransaction_MissingID_Returns400(t *testing.T) {
	setup := buildHandlerSetup("")

	req := httptest.NewRequest(http.MethodGet, "/internal/transaction", nil)
	req = withAuth(req, "user-alice")
	w := doRequest(setup.mux, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status: want 400, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// GET /internal/transaction — 交易不存在返回 404
// ─────────────────────────────────────────────────────────────────

func TestPaymentHandler_GetInternalTransaction_NotFound_Returns404(t *testing.T) {
	setup := buildHandlerSetup("")

	req := httptest.NewRequest(http.MethodGet, "/internal/transaction?id=nonexistent", nil)
	req = withAuth(req, "user-alice")
	w := doRequest(setup.mux, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status: want 404, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// POST /internal/transaction — 错误方法返回 405
// ─────────────────────────────────────────────────────────────────

func TestPaymentHandler_GetInternalTransaction_WrongMethod_Returns405(t *testing.T) {
	setup := buildHandlerSetup("")

	req := httptest.NewRequest(http.MethodPost, "/internal/transaction", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(req, "user-alice")
	w := doRequest(setup.mux, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status: want 405, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// POST /webhooks/recurring-token — 配置了 secret 时必须传正确 X-Webhook-Secret
// ─────────────────────────────────────────────────────────────────

func TestPaymentHandler_WebhookRecurringToken_RequiresSecret(t *testing.T) {
	setup := buildHandlerSetup("whsec_demo")

	body := `{"provider_ref":"pi_x","recurring_token":"tok_r"}`

	req := httptest.NewRequest(http.MethodPost, "/webhooks/recurring-token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := doRequest(setup.mux, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("without X-Webhook-Secret: want 401, got %d", w.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/webhooks/recurring-token", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Webhook-Secret", "wrong")
	w2 := doRequest(setup.mux, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("wrong secret: want 401, got %d", w2.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// POST /webhooks/recurring-token — 缺少必填字段返回 400
// ─────────────────────────────────────────────────────────────────

func TestPaymentHandler_WebhookRecurringToken_MissingFields_Returns400(t *testing.T) {
	setup := buildHandlerSetup("")

	body := `{"provider_ref":"pi_x"}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/recurring-token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := doRequest(setup.mux, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status: want 400, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// GET /webhooks/recurring-token — 错误方法返回 405
// ─────────────────────────────────────────────────────────────────

func TestPaymentHandler_WebhookRecurringToken_WrongMethod_Returns405(t *testing.T) {
	setup := buildHandlerSetup("")

	req := httptest.NewRequest(http.MethodGet, "/webhooks/recurring-token", nil)
	w := doRequest(setup.mux, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status: want 405, got %d", w.Code)
	}
}
