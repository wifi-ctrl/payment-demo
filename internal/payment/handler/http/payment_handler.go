package http

import (
	"encoding/json"
	"log"
	"net/http"

	"payment-demo/internal/identity/handler/middleware"
	"payment-demo/internal/payment/application"
	"payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// PaymentHandler HTTP 驱动适配器
type PaymentHandler struct {
	useCase *application.ChargeUseCase
}

func NewPaymentHandler(uc *application.ChargeUseCase) *PaymentHandler {
	return &PaymentHandler{useCase: uc}
}

// RegisterRoutes 注册路由：
//
//	POST /charge            — Card 支付（多商户）
//	POST /charge/paypal     — PayPal 支付（多商户）
//	POST /capture           — 扣款（transaction_id in body）
//	POST /refund            — 退款（transaction_id in body）
//	GET  /transaction       — 查询交易（id in query string）
func (h *PaymentHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/charge", h.handleCharge)
	mux.HandleFunc("/charge/paypal", h.handlePayPalCharge)
	mux.HandleFunc("/capture", h.handleCapture)
	mux.HandleFunc("/refund", h.handleRefund)
	mux.HandleFunc("/transaction", h.handleGetTransaction)
}

// ─────────────────────────────────────────────────────────────────
// Request / Response DTOs
// ─────────────────────────────────────────────────────────────────

// CaptureRequest POST /capture 请求体
type CaptureRequest struct {
	TransactionID string `json:"transaction_id"`
}

// RefundRequest POST /refund 请求体
type RefundRequest struct {
	TransactionID string `json:"transaction_id"`
}

// PurchaseRequest Card 购买请求。
// merchant_id 必填，标识本次交易归属哪个商户（多商户路由依据）。
// saved_card_id 与 token_id 二选一：
//   - saved_card_id 非空时使用已保存卡（通过 CardQuery ACL 查询 VaultToken）
//   - 否则使用一次性 token_id（前端 tokenization）
type PurchaseRequest struct {
	MerchantID  string `json:"merchant_id"`
	ProductID   string `json:"product_id"`
	TokenID     string `json:"token_id,omitempty"`      // 一次性 Token
	Last4       string `json:"last4,omitempty"`
	Brand       string `json:"brand,omitempty"`
	SavedCardID string `json:"saved_card_id,omitempty"` // 已保存卡 ID
}

// PayPalPurchaseRequest PayPal 购买请求。
// merchant_id 必填。
// order_id / payer_id 均由前端 PayPal JS SDK tokenization 后返回。
type PayPalPurchaseRequest struct {
	MerchantID string `json:"merchant_id"`
	ProductID  string `json:"product_id"`
	OrderID    string `json:"order_id"` // PayPal JS SDK 返回的 Order ID
	PayerID    string `json:"payer_id"` // PayPal JS SDK 返回的 Payer ID
}

// TransactionResponse 交易响应（Card 与 PayPal 共用，method 字段区分）
type TransactionResponse struct {
	ID          string `json:"id"`
	MerchantID  string `json:"merchant_id,omitempty"`
	UserID      string `json:"user_id"`
	ProductID   string `json:"product_id"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	Method      string `json:"method"`
	Status      string `json:"status"`
	ProviderRef string `json:"provider_ref,omitempty"`
	AuthCode    string `json:"auth_code,omitempty"`
	FailReason  string `json:"fail_reason,omitempty"`
}

// ─────────────────────────────────────────────────────────────────
// Handlers
// ─────────────────────────────────────────────────────────────────

func (h *PaymentHandler) handleCharge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PurchaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.MerchantID == "" {
		jsonError(w, "merchant_id is required", http.StatusBadRequest)
		return
	}
	// 校验：saved_card_id 与 token_id 至少提供一个
	if req.SavedCardID == "" && req.TokenID == "" {
		jsonError(w, "saved_card_id or token_id is required", http.StatusBadRequest)
		return
	}

	// userID 来自 identity 的 auth middleware，通过 ctx 传递
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	txn, err := h.useCase.Purchase(r.Context(), application.PurchaseRequest{
		MerchantID:  req.MerchantID,
		UserID:      userID,
		ProductID:   req.ProductID,
		Token:       model.CardToken{TokenID: req.TokenID, Last4: req.Last4, Brand: req.Brand},
		SavedCardID: req.SavedCardID,
	})
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	jsonOK(w, toResponse(txn))
}

// handlePayPalCharge POST /charge/paypal — PayPal 支付入口（多商户）
func (h *PaymentHandler) handlePayPalCharge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PayPalPurchaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.MerchantID == "" {
		jsonError(w, "merchant_id is required", http.StatusBadRequest)
		return
	}
	if req.OrderID == "" || req.PayerID == "" {
		jsonError(w, "order_id and payer_id are required", http.StatusBadRequest)
		return
	}
	if req.ProductID == "" {
		jsonError(w, "product_id is required", http.StatusBadRequest)
		return
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	txn, err := h.useCase.PayPalPurchase(r.Context(), application.PayPalPurchaseRequest{
		MerchantID: req.MerchantID,
		UserID:     userID,
		ProductID:  req.ProductID,
		Token:      model.PayPalToken{OrderID: req.OrderID, PayerID: req.PayerID},
	})
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	jsonOK(w, toResponse(txn))
}

func (h *PaymentHandler) handleCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CaptureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.TransactionID == "" {
		jsonError(w, "transaction_id is required", http.StatusBadRequest)
		return
	}

	txn, err := h.useCase.Capture(r.Context(), model.TransactionID(req.TransactionID))
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	jsonOK(w, toResponse(txn))
}

func (h *PaymentHandler) handleRefund(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RefundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.TransactionID == "" {
		jsonError(w, "transaction_id is required", http.StatusBadRequest)
		return
	}

	txn, err := h.useCase.Refund(r.Context(), model.TransactionID(req.TransactionID))
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	jsonOK(w, toResponse(txn))
}

func (h *PaymentHandler) handleGetTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	txnID := r.URL.Query().Get("id")
	if txnID == "" {
		jsonError(w, "id query parameter is required", http.StatusBadRequest)
		return
	}

	txn, err := h.useCase.GetTransaction(r.Context(), model.TransactionID(txnID))
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	jsonOK(w, toResponse(txn))
}

// ─────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────

func toResponse(txn *model.PaymentTransaction) TransactionResponse {
	return TransactionResponse{
		ID:          string(txn.ID),
		MerchantID:  txn.MerchantID,
		UserID:      txn.UserID,
		ProductID:   txn.ProductID,
		Amount:      txn.Amount.Amount,
		Currency:    txn.Amount.Currency,
		Method:      string(txn.Method),
		Status:      string(txn.Status),
		ProviderRef: txn.ProviderRef,
		AuthCode:    txn.AuthCode,
		FailReason:  txn.FailReason,
	}
}

// mapErrorStatus 错误 → HTTP 状态码映射，统一管理。
func mapErrorStatus(err error) int {
	switch err {
	case model.ErrTransactionNotFound, model.ErrProductNotFound, model.ErrCardNotFound:
		return http.StatusNotFound
	case port.ErrMerchantCredentialNotFound:
		return http.StatusNotFound
	case model.ErrInvalidStateTransition:
		return http.StatusConflict
	case model.ErrProductNotActive, model.ErrCardNotUsable, model.ErrMerchantRequired:
		return http.StatusBadRequest
	case model.ErrAuthorizationDeclined,
		model.ErrPayPalTokenInvalid,
		model.ErrPayPalOrderMismatch:
		return http.StatusUnprocessableEntity
	case model.ErrMerchantGatewayBuildFailed:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

func jsonOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("[PaymentHandler] jsonOK encode error: %v", err)
	}
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		log.Printf("[PaymentHandler] jsonError encode error: %v", err)
	}
}
