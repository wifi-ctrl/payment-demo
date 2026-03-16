package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"payment-demo/internal/identity/handler/middleware"
	"payment-demo/internal/payment/application"
	"payment-demo/internal/payment/domain/model"
)

// PaymentHandler HTTP 驱动适配器
type PaymentHandler struct {
	useCase *application.ChargeUseCase
}

func NewPaymentHandler(uc *application.ChargeUseCase) *PaymentHandler {
	return &PaymentHandler{useCase: uc}
}

// RegisterRoutes 注册路由
func (h *PaymentHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/charge", h.handleCharge)
	mux.HandleFunc("/capture/", h.handleCapture)
	mux.HandleFunc("/refund/", h.handleRefund)
	mux.HandleFunc("/transaction/", h.handleGetTransaction)
}

// --- Request / Response DTOs ---

// PurchaseRequest 购买请求
// saved_card_id 与 token_id 二选一：
//   - saved_card_id 非空时使用已保存卡（通过 CardQuery ACL 查询 VaultToken）
//   - 否则使用一次性 token_id（前端 tokenization）
type PurchaseRequest struct {
	ProductID   string `json:"product_id"`
	TokenID     string `json:"token_id,omitempty"`    // 一次性 Token
	Last4       string `json:"last4,omitempty"`
	Brand       string `json:"brand,omitempty"`
	SavedCardID string `json:"saved_card_id,omitempty"` // 已保存卡 ID
}

type TransactionResponse struct {
	ID          string `json:"id"`
	UserID      string `json:"user_id"`
	ProductID   string `json:"product_id"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	Status      string `json:"status"`
	ProviderRef string `json:"provider_ref,omitempty"`
	AuthCode    string `json:"auth_code,omitempty"`
	FailReason  string `json:"fail_reason,omitempty"`
}

// --- Handlers ---

func (h *PaymentHandler) handleCharge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PurchaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// 校验：saved_card_id 与 token_id 至少提供一个
	if req.SavedCardID == "" && req.TokenID == "" {
		jsonError(w, "saved_card_id or token_id is required", http.StatusBadRequest)
		return
	}

	// userID 来自 identity 的 auth middleware，通过 ctx 传递
	// payment handler 不 import identity 的 domain，只取一个 string
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	txn, err := h.useCase.Purchase(r.Context(), application.PurchaseRequest{
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

func (h *PaymentHandler) handleCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	txnID := extractID(r.URL.Path, "/capture/")
	txn, err := h.useCase.Capture(r.Context(), model.TransactionID(txnID))
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	jsonOK(w, toResponse(txn))
}

func (h *PaymentHandler) handleRefund(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	txnID := extractID(r.URL.Path, "/refund/")
	txn, err := h.useCase.Refund(r.Context(), model.TransactionID(txnID))
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	jsonOK(w, toResponse(txn))
}

func (h *PaymentHandler) handleGetTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	txnID := extractID(r.URL.Path, "/transaction/")
	txn, err := h.useCase.GetTransaction(r.Context(), model.TransactionID(txnID))
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	jsonOK(w, toResponse(txn))
}

// --- Helpers ---

func extractID(path, prefix string) string {
	return strings.TrimPrefix(path, prefix)
}

func toResponse(txn *model.PaymentTransaction) TransactionResponse {
	return TransactionResponse{
		ID:          string(txn.ID),
		UserID:      txn.UserID,
		ProductID:   txn.ProductID,
		Amount:      txn.Amount.Amount,
		Currency:    txn.Amount.Currency,
		Status:      string(txn.Status),
		ProviderRef: txn.ProviderRef,
		AuthCode:    txn.AuthCode,
		FailReason:  txn.FailReason,
	}
}

func mapErrorStatus(err error) int {
	switch err {
	case model.ErrTransactionNotFound, model.ErrProductNotFound, model.ErrCardNotFound:
		return http.StatusNotFound
	case model.ErrInvalidStateTransition:
		return http.StatusConflict
	case model.ErrProductNotActive, model.ErrCardNotUsable:
		return http.StatusBadRequest
	case model.ErrAuthorizationDeclined:
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

func jsonOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
