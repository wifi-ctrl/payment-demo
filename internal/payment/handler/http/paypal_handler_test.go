package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"payment-demo/internal/shared/auth"
	"payment-demo/internal/payment/adapter/persistence"
	"payment-demo/internal/payment/application"
	paymentModel "payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
	paymentHTTP "payment-demo/internal/payment/handler/http"
)

// ─────────────────────────────────────────────────────────────────
// Test Doubles（Handler 测试专用）
// ─────────────────────────────────────────────────────────────────

// handlerStubCatalog 可控制 FindProduct 返回值
type handlerStubCatalog struct {
	product *port.ProductView
	err     error
}

func (c *handlerStubCatalog) FindProduct(_ context.Context, _ string) (*port.ProductView, error) {
	return c.product, c.err
}

// handlerStubCardGateway 不调用真实网关
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

// handlerStubPayPalGateway 可控制行为的 PayPal gateway stub
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

// handlerStubCardQuery 不调用真实 card 上下文
type handlerStubCardQuery struct{}

func (q *handlerStubCardQuery) FindActiveCard(_ context.Context, _ string) (*port.SavedCardView, error) {
	return nil, paymentModel.ErrCardNotFound
}

// ─────────────────────────────────────────────────────────────────
// 多商户桩（Handler 测试层）
// ─────────────────────────────────────────────────────────────────

// handlerMerchantQuery 根据渠道返回对应凭据
type handlerMerchantQuery struct {
	cred *port.ChannelCredentialView
	err  error
}

func (q *handlerMerchantQuery) FindActiveCredential(_ context.Context, _ string, _ paymentModel.PaymentMethod) (*port.ChannelCredentialView, error) {
	return q.cred, q.err
}

// handlerGatewayFactory 按渠道返回预设 Gateway
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

// defaultMerchantCred 默认测试凭据
func defaultMerchantCred() *port.ChannelCredentialView {
	return &port.ChannelCredentialView{
		CredentialID: "cred-1",
		MerchantID:   "merchant-1",
		Channel:      "CARD",
		Secrets:      map[string]string{"api_key": "sk_test_xxx"},
	}
}

// ─────────────────────────────────────────────────────────────────
// 辅助：构建可测试的 HTTP handler + mux
// ─────────────────────────────────────────────────────────────────

type handlerTestSetup struct {
	mux      *http.ServeMux
	repo     *persistence.InMemoryTransactionRepository
	paypalGw *handlerStubPayPalGateway
	cardGw   *handlerStubCardGateway
	catalog  *handlerStubCatalog
}

func buildHandlerSetup(catalog *handlerStubCatalog, paypalGw *handlerStubPayPalGateway) *handlerTestSetup {
	repo := persistence.NewInMemoryTransactionRepository()
	cardGw := &handlerStubCardGateway{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "pi_card_001", AuthCode: "AUTH"},
	}

	merchantQ := &handlerMerchantQuery{cred: defaultMerchantCred()}
	factory := &handlerGatewayFactory{cardGw: cardGw, paypalGw: paypalGw}

	uc := application.NewChargeUseCase(merchantQ, factory, repo, catalog, &handlerStubCardQuery{}, nil, nil)
	handler := paymentHTTP.NewPaymentHandler(uc)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	return &handlerTestSetup{
		mux:      mux,
		repo:     repo,
		paypalGw: paypalGw,
		cardGw:   cardGw,
		catalog:  catalog,
	}
}

// withAuth 在请求 context 中注入 userID（模拟 AuthMiddleware 效果）
func withAuth(r *http.Request, userID string) *http.Request {
	ctx := auth.WithUserID(r.Context(), userID)
	return r.WithContext(ctx)
}

// activeHandlerProduct 返回激活商品
func activeHandlerProduct() *port.ProductView {
	return &port.ProductView{
		ID: "p1", Name: "Widget",
		Amount: 1000, Currency: "USD",
		IsActive: true,
	}
}

// doRequest 执行 HTTP 请求并返回 ResponseRecorder
func doRequest(mux *http.ServeMux, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// parseJSON 解析响应体为 map
func parseJSON(t *testing.T, body string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("parseJSON: %v (body=%q)", err, body)
	}
	return m
}

// ─────────────────────────────────────────────────────────────────
// AC-30: POST /charge/paypal — 正常授权返回 200
// ─────────────────────────────────────────────────────────────────

func TestPayPalHandler_Charge_Success_Returns200(t *testing.T) {
	setup := buildHandlerSetup(
		&handlerStubCatalog{product: activeHandlerProduct()},
		&handlerStubPayPalGateway{
			authorizeResult: &port.PayPalAuthResult{
				ProviderRef: "CAPTURE-001",
				PayerEmail:  "buyer@example.com",
			},
		},
	)

	body := `{"merchant_id":"merchant-1","product_id":"p1","order_id":"5O190127TN364715T","payer_id":"FSMVU44LF3YUS"}`
	req := httptest.NewRequest(http.MethodPost, "/charge/paypal", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(req, "user-alice")

	w := doRequest(setup.mux, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type: want application/json, got %s", ct)
	}

	resp := parseJSON(t, w.Body.String())
	if resp["id"] == nil || resp["id"] == "" {
		t.Error("response must contain non-empty 'id' field")
	}
	if resp["status"] != "AUTHORIZED" {
		t.Errorf("status: want AUTHORIZED, got %v", resp["status"])
	}
	if resp["method"] != "PAYPAL" {
		t.Errorf("method: want PAYPAL, got %v", resp["method"])
	}
	if resp["merchant_id"] != "merchant-1" {
		t.Errorf("merchant_id: want merchant-1, got %v", resp["merchant_id"])
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-31: POST /charge/paypal — 无 Authorization 头，返回 401
// ─────────────────────────────────────────────────────────────────

func TestPayPalHandler_Charge_NoAuthHeader_Returns401(t *testing.T) {
	setup := buildHandlerSetup(
		&handlerStubCatalog{product: activeHandlerProduct()},
		&handlerStubPayPalGateway{authorizeResult: &port.PayPalAuthResult{ProviderRef: "CAPTURE-001"}},
	)

	body := `{"merchant_id":"merchant-1","product_id":"p1","order_id":"5O190127TN364715T","payer_id":"FSMVU44LF3YUS"}`
	req := httptest.NewRequest(http.MethodPost, "/charge/paypal", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// 不注入 userID

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
// 新增: POST /charge/paypal — 缺少 merchant_id，返回 400
// ─────────────────────────────────────────────────────────────────

func TestPayPalHandler_Charge_MissingMerchantID_Returns400(t *testing.T) {
	setup := buildHandlerSetup(
		&handlerStubCatalog{product: activeHandlerProduct()},
		&handlerStubPayPalGateway{},
	)

	body := `{"product_id":"p1","order_id":"5O190127TN364715T","payer_id":"FSMVU44LF3YUS"}` // 缺 merchant_id
	req := httptest.NewRequest(http.MethodPost, "/charge/paypal", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(req, "user-alice")

	w := doRequest(setup.mux, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status: want 400, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-33: POST /charge/paypal — 缺少 order_id，返回 400
// ─────────────────────────────────────────────────────────────────

func TestPayPalHandler_Charge_MissingOrderID_Returns400(t *testing.T) {
	setup := buildHandlerSetup(
		&handlerStubCatalog{product: activeHandlerProduct()},
		&handlerStubPayPalGateway{},
	)

	body := `{"merchant_id":"merchant-1","product_id":"p1","payer_id":"FSMVU44LF3YUS"}` // 缺少 order_id
	req := httptest.NewRequest(http.MethodPost, "/charge/paypal", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(req, "user-alice")

	w := doRequest(setup.mux, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status: want 400, got %d", w.Code)
	}
	resp := parseJSON(t, w.Body.String())
	if errMsg, ok := resp["error"].(string); !ok || errMsg == "" {
		t.Errorf("response must contain non-empty 'error' field, got %v", resp["error"])
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-34: POST /charge/paypal — 缺少 payer_id，返回 400
// ─────────────────────────────────────────────────────────────────

func TestPayPalHandler_Charge_MissingPayerID_Returns400(t *testing.T) {
	setup := buildHandlerSetup(
		&handlerStubCatalog{product: activeHandlerProduct()},
		&handlerStubPayPalGateway{},
	)

	body := `{"merchant_id":"merchant-1","product_id":"p1","order_id":"5O190127TN364715T"}` // 缺少 payer_id
	req := httptest.NewRequest(http.MethodPost, "/charge/paypal", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(req, "user-alice")

	w := doRequest(setup.mux, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status: want 400, got %d", w.Code)
	}
	resp := parseJSON(t, w.Body.String())
	if resp["error"] == nil {
		t.Error("response must contain 'error' field")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-35: POST /charge/paypal — 商品不存在，返回 404
// ─────────────────────────────────────────────────────────────────

func TestPayPalHandler_Charge_ProductNotFound_Returns404(t *testing.T) {
	setup := buildHandlerSetup(
		&handlerStubCatalog{err: paymentModel.ErrProductNotFound},
		&handlerStubPayPalGateway{},
	)

	body := `{"merchant_id":"merchant-1","product_id":"nonexistent","order_id":"5O190127TN364715T","payer_id":"FSMVU44LF3YUS"}`
	req := httptest.NewRequest(http.MethodPost, "/charge/paypal", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(req, "user-alice")

	w := doRequest(setup.mux, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status: want 404, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-36: POST /charge/paypal — PayPal Token 无效，返回 422
// ─────────────────────────────────────────────────────────────────

func TestPayPalHandler_Charge_TokenDeclined_Returns422(t *testing.T) {
	setup := buildHandlerSetup(
		&handlerStubCatalog{product: activeHandlerProduct()},
		&handlerStubPayPalGateway{
			authorizeErr: paymentModel.ErrPayPalTokenInvalid,
		},
	)

	body := `{"merchant_id":"merchant-1","product_id":"p1","order_id":"EC-DECLINE-001","payer_id":"PAYER"}`
	req := httptest.NewRequest(http.MethodPost, "/charge/paypal", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(req, "user-alice")

	w := doRequest(setup.mux, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("Status: want 422, got %d (body=%s)", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.String())
	if resp["error"] == nil {
		t.Error("response must contain 'error' field")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-37: GET /charge/paypal — 错误 HTTP 方法，返回 405
// ─────────────────────────────────────────────────────────────────

func TestPayPalHandler_Charge_WrongMethod_Returns405(t *testing.T) {
	setup := buildHandlerSetup(
		&handlerStubCatalog{product: activeHandlerProduct()},
		&handlerStubPayPalGateway{},
	)

	req := httptest.NewRequest(http.MethodGet, "/charge/paypal", nil)
	req = withAuth(req, "user-alice")

	w := doRequest(setup.mux, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status: want 405, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-38: POST /capture/{id} — PayPal 交易扣款成功，返回 200 + CAPTURED
// ─────────────────────────────────────────────────────────────────

func TestPayPalHandler_Capture_PayPalTransaction_Returns200(t *testing.T) {
	paypalGw := &handlerStubPayPalGateway{
		authorizeResult: &port.PayPalAuthResult{ProviderRef: "CAPTURE-999"},
	}
	setup := buildHandlerSetup(&handlerStubCatalog{product: activeHandlerProduct()}, paypalGw)

	// 先通过 HTTP 创建 PayPal 授权交易
	chargeBody := `{"merchant_id":"merchant-1","product_id":"p1","order_id":"5O190127TN364715T","payer_id":"FSMVU44LF3YUS"}`
	chargeReq := httptest.NewRequest(http.MethodPost, "/charge/paypal", strings.NewReader(chargeBody))
	chargeReq.Header.Set("Content-Type", "application/json")
	chargeReq = withAuth(chargeReq, "user-alice")
	chargeW := doRequest(setup.mux, chargeReq)

	if chargeW.Code != http.StatusOK {
		t.Fatalf("charge failed: %d %s", chargeW.Code, chargeW.Body.String())
	}
	chargeResp := parseJSON(t, chargeW.Body.String())
	txnID, ok := chargeResp["id"].(string)
	if !ok || txnID == "" {
		t.Fatalf("missing txn id in charge response: %v", chargeResp)
	}

	// 执行 Capture
	captureReq := httptest.NewRequest(http.MethodPost, "/capture", strings.NewReader(`{"transaction_id":"`+txnID+`"}`))
	captureReq.Header.Set("Content-Type", "application/json")
	captureReq = withAuth(captureReq, "user-alice")
	captureW := doRequest(setup.mux, captureReq)

	if captureW.Code != http.StatusOK {
		t.Errorf("Capture Status: want 200, got %d (body=%s)", captureW.Code, captureW.Body.String())
	}
	captureResp := parseJSON(t, captureW.Body.String())
	if captureResp["status"] != "CAPTURED" {
		t.Errorf("status: want CAPTURED, got %v", captureResp["status"])
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-39: POST /charge/paypal — JSON 格式错误，返回 400
// ─────────────────────────────────────────────────────────────────

func TestPayPalHandler_Charge_InvalidJSON_Returns400(t *testing.T) {
	setup := buildHandlerSetup(
		&handlerStubCatalog{product: activeHandlerProduct()},
		&handlerStubPayPalGateway{},
	)

	req := httptest.NewRequest(http.MethodPost, "/charge/paypal", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(req, "user-alice")

	w := doRequest(setup.mux, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status: want 400, got %d", w.Code)
	}
	resp := parseJSON(t, w.Body.String())
	if errMsg, ok := resp["error"].(string); !ok || errMsg == "" {
		t.Error("response must contain non-empty 'error' field")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-32: POST /charge/paypal — context 无 userID，返回 401
// ─────────────────────────────────────────────────────────────────

func TestPayPalHandler_Charge_NoUserIDInContext_Returns401(t *testing.T) {
	setup := buildHandlerSetup(
		&handlerStubCatalog{product: activeHandlerProduct()},
		&handlerStubPayPalGateway{},
	)

	body := `{"merchant_id":"merchant-1","product_id":"p1","order_id":"5O190127TN364715T","payer_id":"FSMVU44LF3YUS"}`
	req := httptest.NewRequest(http.MethodPost, "/charge/paypal", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// 不调用 withAuth

	w := doRequest(setup.mux, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status: want 401, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// 补充: POST /charge/paypal — 商品未上架，返回 400
// ─────────────────────────────────────────────────────────────────

func TestPayPalHandler_Charge_ProductNotActive_Returns400(t *testing.T) {
	inactiveProduct := activeHandlerProduct()
	inactiveProduct.IsActive = false

	setup := buildHandlerSetup(
		&handlerStubCatalog{product: inactiveProduct},
		&handlerStubPayPalGateway{},
	)

	body := `{"merchant_id":"merchant-1","product_id":"p1","order_id":"5O190127TN364715T","payer_id":"FSMVU44LF3YUS"}`
	req := httptest.NewRequest(http.MethodPost, "/charge/paypal", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(req, "user-alice")

	w := doRequest(setup.mux, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status: want 400, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// 补充: POST /refund/{id} — PayPal 交易退款成功，返回 200 + REFUNDED（端到端）
// ─────────────────────────────────────────────────────────────────

func TestPayPalHandler_Refund_PayPalTransaction_Returns200(t *testing.T) {
	paypalGw := &handlerStubPayPalGateway{
		authorizeResult: &port.PayPalAuthResult{ProviderRef: "CAPTURE-777"},
	}
	setup := buildHandlerSetup(&handlerStubCatalog{product: activeHandlerProduct()}, paypalGw)

	// 1. 授权
	chargeBody := `{"merchant_id":"merchant-1","product_id":"p1","order_id":"5O190127TN364715T","payer_id":"FSMVU44LF3YUS"}`
	chargeReq := httptest.NewRequest(http.MethodPost, "/charge/paypal", strings.NewReader(chargeBody))
	chargeReq.Header.Set("Content-Type", "application/json")
	chargeReq = withAuth(chargeReq, "user-alice")
	chargeW := doRequest(setup.mux, chargeReq)
	if chargeW.Code != http.StatusOK {
		t.Fatalf("charge: %d %s", chargeW.Code, chargeW.Body.String())
	}
	txnID := parseJSON(t, chargeW.Body.String())["id"].(string)

	// 2. 扣款
	captureReq := httptest.NewRequest(http.MethodPost, "/capture", strings.NewReader(`{"transaction_id":"`+txnID+`"}`))
	captureReq.Header.Set("Content-Type", "application/json")
	captureReq = withAuth(captureReq, "user-alice")
	captureW := doRequest(setup.mux, captureReq)
	if captureW.Code != http.StatusOK {
		t.Fatalf("capture: %d %s", captureW.Code, captureW.Body.String())
	}

	// 3. 退款
	refundReq := httptest.NewRequest(http.MethodPost, "/refund", strings.NewReader(`{"transaction_id":"`+txnID+`"}`))
	refundReq.Header.Set("Content-Type", "application/json")
	refundReq = withAuth(refundReq, "user-alice")
	refundW := doRequest(setup.mux, refundReq)

	if refundW.Code != http.StatusOK {
		t.Errorf("Refund Status: want 200, got %d (body=%s)", refundW.Code, refundW.Body.String())
	}
	refundResp := parseJSON(t, refundW.Body.String())
	if refundResp["status"] != "REFUNDED" {
		t.Errorf("status: want REFUNDED, got %v", refundResp["status"])
	}
}

// ─────────────────────────────────────────────────────────────────
// 补充: GET /transaction/{id} — 查询 PayPal 交易，method 字段为 PAYPAL
// ─────────────────────────────────────────────────────────────────

func TestPayPalHandler_GetTransaction_ReturnsMethodPayPal(t *testing.T) {
	paypalGw := &handlerStubPayPalGateway{
		authorizeResult: &port.PayPalAuthResult{ProviderRef: "CAPTURE-888"},
	}
	setup := buildHandlerSetup(&handlerStubCatalog{product: activeHandlerProduct()}, paypalGw)

	// 先授权
	chargeBody := `{"merchant_id":"merchant-1","product_id":"p1","order_id":"5O190127TN364715T","payer_id":"FSMVU44LF3YUS"}`
	chargeReq := httptest.NewRequest(http.MethodPost, "/charge/paypal", strings.NewReader(chargeBody))
	chargeReq.Header.Set("Content-Type", "application/json")
	chargeReq = withAuth(chargeReq, "user-alice")
	chargeW := doRequest(setup.mux, chargeReq)
	if chargeW.Code != http.StatusOK {
		t.Fatalf("charge: %d %s", chargeW.Code, chargeW.Body.String())
	}
	txnID := parseJSON(t, chargeW.Body.String())["id"].(string)

	// 查询交易
	getReq := httptest.NewRequest(http.MethodGet, "/transaction?id="+txnID, nil)
	getReq = withAuth(getReq, "user-alice")
	getW := doRequest(setup.mux, getReq)

	if getW.Code != http.StatusOK {
		t.Errorf("GetTransaction Status: want 200, got %d", getW.Code)
	}
	resp := parseJSON(t, getW.Body.String())
	if resp["method"] != "PAYPAL" {
		t.Errorf("method: want PAYPAL, got %v", resp["method"])
	}
	if resp["id"] != txnID {
		t.Errorf("id: want %s, got %v", txnID, resp["id"])
	}
}
