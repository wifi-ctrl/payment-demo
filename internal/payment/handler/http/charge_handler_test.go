package http_test

// charge_handler_test.go — Card 渠道多商户 POST /charge Handler 测试
//
// 复用 paypal_handler_test.go 中已定义的：
//   - handlerStubCatalog, handlerStubCardGateway, handlerStubPayPalGateway
//   - handlerMerchantQuery, handlerGatewayFactory
//   - handlerStubCardQuery
//   - doRequest(mux, r), parseJSON(t, body)
//   - withAuth(r, userID), defaultMerchantCred()
//   - activeHandlerProduct()
//
// 本文件只定义 Card 渠道测试专用的辅助函数和测试。

import (
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
// Card 渠道 Handler 测试辅助（与 paypal_handler_test.go 不重复）
// ─────────────────────────────────────────────────────────────────

// buildCardChargeSetup 组装 Card 支付场景的完整测试装配，返回 mux
func buildCardChargeSetup(
	merchantQ *handlerMerchantQuery,
	catalog *handlerStubCatalog,
	cardGw *handlerStubCardGateway,
) *http.ServeMux {
	repo := persistence.NewInMemoryTransactionRepository()
	paypalGw := &handlerStubPayPalGateway{
		authorizeResult: &port.PayPalAuthResult{ProviderRef: "PAYPAL-001"},
	}
	factory := &handlerGatewayFactory{cardGw: cardGw, paypalGw: paypalGw}
	uc := application.NewChargeUseCase(merchantQ, factory, repo, catalog, &handlerStubCardQuery{}, &handlerStubCardCommand{}, nil, nil)
	handler := paymentHTTP.NewPaymentHandler(uc)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return mux
}

// withCardAuth 附加认证到请求（Card 测试专用命名，避免与 withAuth 混淆）
func withCardAuth(r *http.Request, userID string) *http.Request {
	ctx := auth.WithUserID(r.Context(), userID)
	return r.WithContext(ctx)
}

// ─────────────────────────────────────────────────────────────────
// AC-30（Card）: POST /charge — 正常 Card 支付成功返回 200
// ─────────────────────────────────────────────────────────────────

func TestChargeHandler_Charge_Success_Returns200(t *testing.T) {
	// AC-30 Card 渠道版
	merchantQ := &handlerMerchantQuery{cred: &port.ChannelCredentialView{
		CredentialID: "cred-card",
		MerchantID:   "merchant-1",
		Channel:      "CARD",
		Secrets:      map[string]string{"api_key": "sk_live_xxx"},
	}}
	cardGw := &handlerStubCardGateway{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "ch_001", AuthCode: "AUTH-001"},
	}
	mux := buildCardChargeSetup(merchantQ, &handlerStubCatalog{product: activeHandlerProduct()}, cardGw)

	body := `{"merchant_id":"merchant-1","product_id":"p1","token_id":"tok_visa","last4":"4242","brand":"Visa"}`
	req := httptest.NewRequest(http.MethodPost, "/charge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withCardAuth(req, "user-1")

	w := doRequest(mux, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: want 200, got %d — body: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.String())
	if resp["status"] != "AUTHORIZED" {
		t.Errorf("status: want AUTHORIZED, got %v", resp["status"])
	}
	if resp["method"] != "CARD" {
		t.Errorf("method: want CARD, got %v", resp["method"])
	}
	if resp["merchant_id"] != "merchant-1" {
		t.Errorf("merchant_id: want merchant-1, got %v", resp["merchant_id"])
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-32（Card）: POST /charge — 无认证（无 userID in ctx）返回 401
// ─────────────────────────────────────────────────────────────────

func TestChargeHandler_Charge_NoAuth_Returns401(t *testing.T) {
	// AC-32
	merchantQ := &handlerMerchantQuery{cred: defaultMerchantCred()}
	cardGw := &handlerStubCardGateway{authorizeResult: &port.GatewayAuthResult{ProviderRef: "ch_001"}}
	mux := buildCardChargeSetup(merchantQ, &handlerStubCatalog{product: activeHandlerProduct()}, cardGw)

	body := `{"merchant_id":"merchant-1","product_id":"p1","token_id":"tok_visa"}`
	req := httptest.NewRequest(http.MethodPost, "/charge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// 不注入 userID

	w := doRequest(mux, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: want 401, got %d", w.Code)
	}
	resp := parseJSON(t, w.Body.String())
	if resp["error"] == nil {
		t.Error("response must contain 'error' field")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-38: POST /charge — 缺少 merchant_id 返回 400
// ─────────────────────────────────────────────────────────────────

func TestChargeHandler_Charge_MissingMerchantID_Returns400(t *testing.T) {
	// AC-38
	merchantQ := &handlerMerchantQuery{cred: defaultMerchantCred()}
	cardGw := &handlerStubCardGateway{authorizeResult: &port.GatewayAuthResult{ProviderRef: "ch_001"}}
	mux := buildCardChargeSetup(merchantQ, &handlerStubCatalog{product: activeHandlerProduct()}, cardGw)

	// 缺少 merchant_id
	body := `{"product_id":"p1","token_id":"tok_visa","last4":"4242","brand":"Visa"}`
	req := httptest.NewRequest(http.MethodPost, "/charge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withCardAuth(req, "user-1")

	w := doRequest(mux, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: want 400, got %d — body: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.String())
	errMsg, ok := resp["error"].(string)
	if !ok || errMsg == "" {
		t.Errorf("response must contain non-empty 'error' field, got %v", resp["error"])
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-38 补充: POST /charge — token_id 和 saved_card_id 均为空返回 400
// ─────────────────────────────────────────────────────────────────

func TestChargeHandler_Charge_MissingTokenAndSavedCard_Returns400(t *testing.T) {
	merchantQ := &handlerMerchantQuery{cred: defaultMerchantCred()}
	cardGw := &handlerStubCardGateway{authorizeResult: &port.GatewayAuthResult{ProviderRef: "ch_001"}}
	mux := buildCardChargeSetup(merchantQ, &handlerStubCatalog{product: activeHandlerProduct()}, cardGw)

	// 有 merchant_id 但无 token_id 也无 saved_card_id
	body := `{"merchant_id":"merchant-1","product_id":"p1"}`
	req := httptest.NewRequest(http.MethodPost, "/charge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withCardAuth(req, "user-1")

	w := doRequest(mux, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: want 400, got %d — body: %s", w.Code, w.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-39: POST /charge — MerchantQuery 找不到凭据，payment handler 映射为 404
// ─────────────────────────────────────────────────────────────────

func TestChargeHandler_Charge_MerchantCredentialNotFound_Returns404(t *testing.T) {
	// AC-39: ErrMerchantCredentialNotFound → 404（见 mapErrorStatus）
	merchantQ := &handlerMerchantQuery{err: port.ErrMerchantCredentialNotFound}
	cardGw := &handlerStubCardGateway{}
	mux := buildCardChargeSetup(merchantQ, &handlerStubCatalog{product: activeHandlerProduct()}, cardGw)

	body := `{"merchant_id":"merchant-1","product_id":"p1","token_id":"tok_visa","last4":"4242","brand":"Visa"}`
	req := httptest.NewRequest(http.MethodPost, "/charge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withCardAuth(req, "user-1")

	w := doRequest(mux, req)

	// payment_handler.go: ErrMerchantCredentialNotFound → http.StatusNotFound
	if w.Code != http.StatusNotFound {
		t.Errorf("status: want 404, got %d — body: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.String())
	if resp["error"] == nil {
		t.Error("response must contain 'error' field")
	}
}

// ─────────────────────────────────────────────────────────────────
// 额外：POST /charge — Card Gateway 授权被拒绝返回 422
// ─────────────────────────────────────────────────────────────────

func TestChargeHandler_Charge_AuthorizationDeclined_Returns422(t *testing.T) {
	merchantQ := &handlerMerchantQuery{cred: defaultMerchantCred()}
	cardGw := &handlerStubCardGateway{authorizeErr: paymentModel.ErrAuthorizationDeclined}
	mux := buildCardChargeSetup(merchantQ, &handlerStubCatalog{product: activeHandlerProduct()}, cardGw)

	body := `{"merchant_id":"merchant-1","product_id":"p1","token_id":"tok_decline"}`
	req := httptest.NewRequest(http.MethodPost, "/charge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withCardAuth(req, "user-1")

	w := doRequest(mux, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status: want 422, got %d — body: %s", w.Code, w.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────
// 额外：POST /charge — GET 方法返回 405
// ─────────────────────────────────────────────────────────────────

func TestChargeHandler_Charge_WrongMethod_Returns405(t *testing.T) {
	merchantQ := &handlerMerchantQuery{cred: defaultMerchantCred()}
	cardGw := &handlerStubCardGateway{}
	mux := buildCardChargeSetup(merchantQ, &handlerStubCatalog{product: activeHandlerProduct()}, cardGw)

	req := httptest.NewRequest(http.MethodGet, "/charge", nil)
	req = withCardAuth(req, "user-1")

	w := doRequest(mux, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: want 405, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// 额外：POST /charge — 商品不存在返回 404
// ─────────────────────────────────────────────────────────────────

func TestChargeHandler_Charge_ProductNotFound_Returns404(t *testing.T) {
	merchantQ := &handlerMerchantQuery{cred: defaultMerchantCred()}
	cardGw := &handlerStubCardGateway{}
	mux := buildCardChargeSetup(merchantQ, &handlerStubCatalog{err: paymentModel.ErrProductNotFound}, cardGw)

	body := `{"merchant_id":"merchant-1","product_id":"no-such","token_id":"tok_visa"}`
	req := httptest.NewRequest(http.MethodPost, "/charge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withCardAuth(req, "user-1")

	w := doRequest(mux, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: want 404, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// 额外：POST /charge → capture → refund 端到端流程（Card 渠道多商户）
// ─────────────────────────────────────────────────────────────────

func TestChargeHandler_CardPurchaseCaptureRefund_E2E(t *testing.T) {
	merchantQ := &handlerMerchantQuery{cred: &port.ChannelCredentialView{
		CredentialID: "cred-card",
		MerchantID:   "merchant-1",
		Channel:      "CARD",
		Secrets:      map[string]string{"api_key": "sk_live_xxx"},
	}}
	cardGw := &handlerStubCardGateway{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "ch_e2e_001", AuthCode: "AUTH-E2E"},
	}
	mux := buildCardChargeSetup(merchantQ, &handlerStubCatalog{product: activeHandlerProduct()}, cardGw)

	// 1. 购买（授权）
	chargeBody := `{"merchant_id":"merchant-1","product_id":"p1","token_id":"tok_visa","last4":"4242","brand":"Visa"}`
	chargeReq := httptest.NewRequest(http.MethodPost, "/charge", strings.NewReader(chargeBody))
	chargeReq.Header.Set("Content-Type", "application/json")
	chargeReq = withCardAuth(chargeReq, "user-1")
	chargeW := doRequest(mux, chargeReq)
	if chargeW.Code != http.StatusOK {
		t.Fatalf("charge: %d — %s", chargeW.Code, chargeW.Body.String())
	}
	txnID, ok := parseJSON(t, chargeW.Body.String())["id"].(string)
	if !ok || txnID == "" {
		t.Fatal("charge: missing txn id")
	}

	// 2. Capture
	captureReq := httptest.NewRequest(http.MethodPost, "/capture", strings.NewReader(`{"transaction_id":"`+txnID+`"}`))
	captureReq.Header.Set("Content-Type", "application/json")
	captureReq = withCardAuth(captureReq, "user-1")
	captureW := doRequest(mux, captureReq)
	if captureW.Code != http.StatusOK {
		t.Fatalf("capture: %d — %s", captureW.Code, captureW.Body.String())
	}
	captureResp := parseJSON(t, captureW.Body.String())
	if captureResp["status"] != "CAPTURED" {
		t.Errorf("after capture: want CAPTURED, got %v", captureResp["status"])
	}

	// 3. Refund
	refundReq := httptest.NewRequest(http.MethodPost, "/refund", strings.NewReader(`{"transaction_id":"`+txnID+`"}`))
	refundReq.Header.Set("Content-Type", "application/json")
	refundReq = withCardAuth(refundReq, "user-1")
	refundW := doRequest(mux, refundReq)
	if refundW.Code != http.StatusOK {
		t.Fatalf("refund: %d — %s", refundW.Code, refundW.Body.String())
	}
	refundResp := parseJSON(t, refundW.Body.String())
	if refundResp["status"] != "REFUNDED" {
		t.Errorf("after refund: want REFUNDED, got %v", refundResp["status"])
	}
}

// ─────────────────────────────────────────────────────────────────
// 额外：GET /transaction/{id} — 查询 Card 交易，method 字段为 CARD
// ─────────────────────────────────────────────────────────────────

func TestChargeHandler_GetTransaction_ReturnsMethodCard(t *testing.T) {
	merchantQ := &handlerMerchantQuery{cred: defaultMerchantCred()}
	cardGw := &handlerStubCardGateway{
		authorizeResult: &port.GatewayAuthResult{ProviderRef: "ch_query_001"},
	}
	mux := buildCardChargeSetup(merchantQ, &handlerStubCatalog{product: activeHandlerProduct()}, cardGw)

	// 先授权
	chargeBody := `{"merchant_id":"merchant-1","product_id":"p1","token_id":"tok_visa","last4":"4242","brand":"Visa"}`
	chargeReq := httptest.NewRequest(http.MethodPost, "/charge", strings.NewReader(chargeBody))
	chargeReq.Header.Set("Content-Type", "application/json")
	chargeReq = withCardAuth(chargeReq, "user-1")
	chargeW := doRequest(mux, chargeReq)
	if chargeW.Code != http.StatusOK {
		t.Fatalf("charge: %d %s", chargeW.Code, chargeW.Body.String())
	}
	txnID := parseJSON(t, chargeW.Body.String())["id"].(string)

	// 查询交易
	getReq := httptest.NewRequest(http.MethodGet, "/transaction?id="+txnID, nil)
	getReq = withCardAuth(getReq, "user-1")
	getW := doRequest(mux, getReq)

	if getW.Code != http.StatusOK {
		t.Errorf("GetTransaction: want 200, got %d", getW.Code)
	}
	resp := parseJSON(t, getW.Body.String())
	if resp["method"] != "CARD" {
		t.Errorf("method: want CARD, got %v", resp["method"])
	}
	if resp["id"] != txnID {
		t.Errorf("id: want %s, got %v", txnID, resp["id"])
	}
}

// ─────────────────────────────────────────────────────────────────
// 额外：POST /charge — 无效 JSON 返回 400
// ─────────────────────────────────────────────────────────────────

func TestChargeHandler_Charge_InvalidJSON_Returns400(t *testing.T) {
	merchantQ := &handlerMerchantQuery{cred: defaultMerchantCred()}
	cardGw := &handlerStubCardGateway{}
	mux := buildCardChargeSetup(merchantQ, &handlerStubCatalog{product: activeHandlerProduct()}, cardGw)

	req := httptest.NewRequest(http.MethodPost, "/charge", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	req = withCardAuth(req, "user-1")

	w := doRequest(mux, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: want 400, got %d", w.Code)
	}
}
