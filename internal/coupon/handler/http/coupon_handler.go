// Package http 提供 coupon 上下文的 HTTP 驱动适配器。
package http

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"payment-demo/internal/coupon/application"
	"payment-demo/internal/coupon/domain/model"
)

// CouponHandler HTTP 驱动适配器。
type CouponHandler struct {
	useCase *application.CouponUseCase
}

// NewCouponHandler 构造函数注入用例。
func NewCouponHandler(uc *application.CouponUseCase) *CouponHandler {
	return &CouponHandler{useCase: uc}
}

// RegisterRoutes 注册路由：
//
//	POST /coupons        — 创建优惠券
//	GET  /coupons        — 按 code 查询（query: ?code=SAVE10）
func (h *CouponHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/coupons", h.handleCoupons)
}

// ─────────────────────────────────────────────────────────────────
// Request / Response DTOs
// ─────────────────────────────────────────────────────────────────

// CreateCouponRequest POST /coupons 请求体
type CreateCouponRequest struct {
	Code          string `json:"code"`
	DiscountType  string `json:"discount_type"`  // "PERCENTAGE" | "FIXED"
	DiscountValue int64  `json:"discount_value"`  // PERCENTAGE: basis point；FIXED: cents
	MaxUses       int    `json:"max_uses"`        // 0 = 不限
	ValidFrom     string `json:"valid_from"`      // RFC3339
	ValidUntil    string `json:"valid_until"`     // RFC3339
}

// CouponResponse 优惠券响应
type CouponResponse struct {
	CouponID      string `json:"coupon_id"`
	Code          string `json:"code"`
	DiscountType  string `json:"discount_type"`
	DiscountValue int64  `json:"discount_value"`
	MaxUses       int    `json:"max_uses"`
	UsedCount     int    `json:"used_count"`
	ValidFrom     string `json:"valid_from"`
	ValidUntil    string `json:"valid_until"`
	Status        string `json:"status"`
}

// ─────────────────────────────────────────────────────────────────
// Handlers
// ─────────────────────────────────────────────────────────────────

func (h *CouponHandler) handleCoupons(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleCreateCoupon(w, r)
	case http.MethodGet:
		h.handleGetCoupon(w, r)
	default:
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCreateCoupon POST /coupons — 创建优惠券
func (h *CouponHandler) handleCreateCoupon(w http.ResponseWriter, r *http.Request) {
	var req CreateCouponRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Code == "" {
		jsonError(w, "code is required", http.StatusBadRequest)
		return
	}
	if req.DiscountType == "" {
		jsonError(w, "discount_type is required", http.StatusBadRequest)
		return
	}

	validFrom, err := time.Parse(time.RFC3339, req.ValidFrom)
	if err != nil {
		jsonError(w, "valid_from must be RFC3339 format", http.StatusBadRequest)
		return
	}
	validUntil, err := time.Parse(time.RFC3339, req.ValidUntil)
	if err != nil {
		jsonError(w, "valid_until must be RFC3339 format", http.StatusBadRequest)
		return
	}

	coupon, err := h.useCase.CreateCoupon(r.Context(), application.CreateCouponRequest{
		Code:          req.Code,
		DiscountType:  req.DiscountType,
		DiscountValue: req.DiscountValue,
		MaxUses:       req.MaxUses,
		ValidFrom:     validFrom,
		ValidUntil:    validUntil,
	})
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(toResponse(coupon)); err != nil {
		log.Printf("[CouponHandler] encode error: %v", err)
	}
}

// handleGetCoupon GET /coupons?code=SAVE10 — 按编码查询优惠券
func (h *CouponHandler) handleGetCoupon(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		jsonError(w, "code query parameter is required", http.StatusBadRequest)
		return
	}

	coupon, err := h.useCase.GetCouponByCode(r.Context(), code)
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	jsonOK(w, toResponse(coupon))
}

// ─────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────

func toResponse(c *model.Coupon) CouponResponse {
	return CouponResponse{
		CouponID:      string(c.ID),
		Code:          string(c.Code),
		DiscountType:  string(c.Rule.Type),
		DiscountValue: c.Rule.Value,
		MaxUses:       c.MaxUses,
		UsedCount:     c.UsedCount,
		ValidFrom:     c.ValidFrom.Format(time.RFC3339),
		ValidUntil:    c.ValidUntil.Format(time.RFC3339),
		Status:        string(c.Status),
	}
}

// mapErrorStatus 错误 → HTTP 状态码映射，统一管理。
func mapErrorStatus(err error) int {
	switch err {
	case model.ErrCouponNotFound:
		return http.StatusNotFound
	case model.ErrCouponCodeConflict:
		return http.StatusConflict
	case model.ErrCouponNotApplicable:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func jsonOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("[CouponHandler] jsonOK encode error: %v", err)
	}
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		log.Printf("[CouponHandler] jsonError encode error: %v", err)
	}
}
