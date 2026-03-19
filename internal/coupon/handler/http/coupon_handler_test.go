// Package http_test 为 coupon handler 提供 HTTP 集成测试。
// 使用 httptest + 真实 identity InMemory 仓储 + 真实 AuthMiddleware，
// 通过 token_alice 完成认证。
package http_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	identityPersistence "payment-demo/internal/identity/adapter/persistence"
	identityApp "payment-demo/internal/identity/application"
	identityMW "payment-demo/internal/identity/handler/middleware"

	couponInmem "payment-demo/internal/coupon/adapter/inmem"
	couponApp "payment-demo/internal/coupon/application"
	couponHTTP "payment-demo/internal/coupon/handler/http"
)

// ─────────────────────────────────────────────────────────────────
// 测试服务器
// ─────────────────────────────────────────────────────────────────

func buildCouponServer(t *testing.T) *httptest.Server {
	t.Helper()

	userRepo := identityPersistence.NewInMemoryUserRepository()
	sessionRepo := identityPersistence.NewInMemorySessionRepository()
	authUC := identityApp.NewAuthUseCase(userRepo, sessionRepo)
	authMiddleware := identityMW.NewAuthMiddleware(authUC)

	couponRepo := couponInmem.NewInMemoryCouponRepository()
	uc := couponApp.NewCouponUseCase(couponRepo)
	handler := couponHTTP.NewCouponHandler(uc)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	srv := httptest.NewServer(authMiddleware.Handle(mux))
	t.Cleanup(srv.Close)
	return srv
}

// ─────────────────────────────────────────────────────────────────
// HTTP 请求辅助
// ─────────────────────────────────────────────────────────────────

const testAuthToken = "token_alice"

func postCoupon(t *testing.T, srv *httptest.Server, body interface{}) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/coupons", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testAuthToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func postCouponNoAuth(t *testing.T, srv *httptest.Server, body interface{}) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/coupons", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func getCoupon(t *testing.T, srv *httptest.Server, code string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/coupons?code="+code, nil)
	req.Header.Set("Authorization", "Bearer "+testAuthToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func decodeBody(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	return m
}

// ─────────────────────────────────────────────────────────────────
// 有效优惠券请求体
// ─────────────────────────────────────────────────────────────────

func validCouponBody() map[string]interface{} {
	return map[string]interface{}{
		"code":           "SUMMER20",
		"discount_type":  "PERCENTAGE",
		"discount_value": 2000,
		"max_uses":       50,
		"valid_from":     "2025-01-01T00:00:00Z",
		"valid_until":    "2025-12-31T23:59:59Z",
	}
}

// ═══════════════════════════════════════════════════════════════════
// AC-38: POST /coupons 成功创建
// ═══════════════════════════════════════════════════════════════════

func TestCouponHandler_Create_Success(t *testing.T) {
	// AC-38
	srv := buildCouponServer(t)

	resp := postCoupon(t, srv, validCouponBody())
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201 Created, got %d", resp.StatusCode)
	}

	m := decodeBody(t, resp)
	if m["coupon_id"] == nil || m["coupon_id"].(string) == "" {
		t.Error("expected non-empty coupon_id")
	}
	if m["code"] != "SUMMER20" {
		t.Errorf("expected code=SUMMER20, got %v", m["code"])
	}
	if m["status"] != "ACTIVE" {
		t.Errorf("expected status=ACTIVE, got %v", m["status"])
	}
}

// ═══════════════════════════════════════════════════════════════════
// AC-39: POST /coupons — code 重复返回 409
// ═══════════════════════════════════════════════════════════════════

func TestCouponHandler_Create_DuplicateCode_Returns409(t *testing.T) {
	// AC-39
	srv := buildCouponServer(t)

	// 第一次创建
	resp1 := postCoupon(t, srv, validCouponBody())
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("first create should succeed, got %d", resp1.StatusCode)
	}

	// 第二次创建相同 code
	resp2 := postCoupon(t, srv, validCouponBody())
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 Conflict for duplicate code, got %d", resp2.StatusCode)
	}
}

// ─────────────────────────────────────────────────────────────────
// POST /coupons — 缺少必要字段返回 400
// ─────────────────────────────────────────────────────────────────

func TestCouponHandler_Create_MissingCode_Returns400(t *testing.T) {
	srv := buildCouponServer(t)

	body := map[string]interface{}{
		// 缺少 code
		"discount_type":  "PERCENTAGE",
		"discount_value": 1000,
		"valid_from":     "2025-01-01T00:00:00Z",
		"valid_until":    "2025-12-31T23:59:59Z",
	}

	resp := postCoupon(t, srv, body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for missing code, got %d", resp.StatusCode)
	}
}

// ─────────────────────────────────────────────────────────────────
// POST /coupons — 无认证返回 401
// ─────────────────────────────────────────────────────────────────

func TestCouponHandler_Create_Unauthorized_Returns401(t *testing.T) {
	srv := buildCouponServer(t)

	resp := postCouponNoAuth(t, srv, validCouponBody())
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", resp.StatusCode)
	}
}

// ─────────────────────────────────────────────────────────────────
// GET /coupons?code=xxx — 查询成功
// ─────────────────────────────────────────────────────────────────

func TestCouponHandler_GetByCode_Success(t *testing.T) {
	srv := buildCouponServer(t)

	// 先创建
	resp1 := postCoupon(t, srv, validCouponBody())
	resp1.Body.Close()

	resp := getCoupon(t, srv, "SUMMER20")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}

	m := decodeBody(t, resp)
	if m["code"] != "SUMMER20" {
		t.Errorf("expected code=SUMMER20, got %v", m["code"])
	}
	if m["status"] != "ACTIVE" {
		t.Errorf("expected status=ACTIVE, got %v", m["status"])
	}
}

// ─────────────────────────────────────────────────────────────────
// GET /coupons?code=xxx — 不存在返回 404
// ─────────────────────────────────────────────────────────────────

func TestCouponHandler_GetByCode_NotFound_Returns404(t *testing.T) {
	srv := buildCouponServer(t)

	resp := getCoupon(t, srv, "NOTEXIST")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d", resp.StatusCode)
	}
}

// ─────────────────────────────────────────────────────────────────
// POST /coupons — 无效折扣类型返回 400
// ─────────────────────────────────────────────────────────────────

func TestCouponHandler_Create_InvalidDiscountType_Returns400(t *testing.T) {
	srv := buildCouponServer(t)

	body := map[string]interface{}{
		"code":           "BAD",
		"discount_type":  "UNKNOWN_TYPE",
		"discount_value": 100,
		"valid_from":     "2025-01-01T00:00:00Z",
		"valid_until":    "2025-12-31T23:59:59Z",
	}

	resp := postCoupon(t, srv, body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid discount_type, got %d", resp.StatusCode)
	}
}
