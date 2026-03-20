package http

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"

	"payment-demo/internal/acquiring/application"
	"payment-demo/internal/acquiring/domain/model"
	"payment-demo/internal/acquiring/domain/port"
	"payment-demo/internal/shared/auth"
	"payment-demo/internal/shared/httputil"
)

// PaymentHandler HTTP 驱动适配器（内部端口）。
//
// 引入 Order 上下文后，前端不再直接调用 Payment API。
// 保留以下端点供 内部诊断 / webhook 使用：
//   - GET  /internal/transaction     — 按交易 ID 查询
//   - POST /webhooks/recurring-token — 渠道异步回调
type PaymentHandler struct {
	useCase       *application.ChargeUseCase
	webhookSecret string
}

func NewPaymentHandler(uc *application.ChargeUseCase, recurringWebhookSecret string) *PaymentHandler {
	return &PaymentHandler{useCase: uc, webhookSecret: recurringWebhookSecret}
}

func webhookSecretHeaderOK(headerVal, want string) bool {
	if want == "" {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(headerVal), []byte(want)) == 1
}

// RegisterRoutes 注册保留的内部端点。
func (h *PaymentHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/internal/transaction", h.handleGetTransaction)
	mux.HandleFunc("/webhooks/recurring-token", h.handleWebhookRecurringToken)
}

// ── DTOs ───────────────────────────────────────────────────────

type WebhookRecurringTokenRequest struct {
	ProviderRef    string `json:"provider_ref"`
	Channel        string `json:"channel"`
	RecurringToken string `json:"recurring_token"`
}

type TransactionResponse struct {
	ID          string `json:"id"`
	MerchantID  string `json:"merchant_id,omitempty"`
	UserID      string `json:"user_id"`
	OrderID     string `json:"order_id,omitempty"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	Method      string `json:"method"`
	Status      string `json:"status"`
	ProviderRef string `json:"provider_ref,omitempty"`
	AuthCode    string `json:"auth_code,omitempty"`
	FailReason  string `json:"fail_reason,omitempty"`
}

// ── Handlers ───────────────────────────────────────────────────

func (h *PaymentHandler) handleGetTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	txnID := r.URL.Query().Get("id")
	if txnID == "" {
		httputil.Error(w, "id query parameter is required", http.StatusBadRequest)
		return
	}
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httputil.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	txn, err := h.useCase.GetTransaction(r.Context(), userID, model.TransactionID(txnID))
	if err != nil {
		httputil.UseCaseError(w, err, mapErrorStatus)
		return
	}
	httputil.OK(w, toResponse(txn))
}

func (h *PaymentHandler) handleWebhookRecurringToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req WebhookRecurringTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ProviderRef == "" || req.RecurringToken == "" {
		httputil.Error(w, "provider_ref and recurring_token are required", http.StatusBadRequest)
		return
	}
	if !webhookSecretHeaderOK(r.Header.Get("X-Webhook-Secret"), h.webhookSecret) {
		httputil.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.useCase.HandleRecurringTokenCallback(r.Context(), application.RecurringTokenCallbackRequest{
		ProviderRef:    req.ProviderRef,
		Channel:        req.Channel,
		RecurringToken: req.RecurringToken,
	}); err != nil {
		httputil.UseCaseError(w, err, mapErrorStatus)
		return
	}
	httputil.OK(w, map[string]string{"status": "ok"})
}

// ── Helpers ────────────────────────────────────────────────────

func toResponse(txn *model.PaymentTransaction) TransactionResponse {
	return TransactionResponse{
		ID:          string(txn.ID),
		MerchantID:  txn.MerchantID,
		UserID:      txn.UserID,
		OrderID:     txn.OrderID,
		Amount:      txn.Amount.Amount,
		Currency:    txn.Amount.Currency,
		Method:      string(txn.Method),
		Status:      string(txn.Status),
		ProviderRef: txn.ProviderRef,
		AuthCode:    txn.AuthCode,
		FailReason:  txn.FailReason,
	}
}

func mapErrorStatus(err error) int {
	switch err {
	case model.ErrTransactionNotFound, model.ErrCardNotFound:
		return http.StatusNotFound
	case port.ErrMerchantCredentialNotFound:
		return http.StatusNotFound
	case model.ErrInvalidStateTransition:
		return http.StatusConflict
	case model.ErrCardNotUsable, model.ErrMerchantRequired:
		return http.StatusBadRequest
	case model.ErrAuthorizationDeclined, model.ErrPayPalTokenInvalid, model.ErrPayPalOrderMismatch:
		return http.StatusUnprocessableEntity
	case model.ErrMerchantGatewayBuildFailed:
		return http.StatusInternalServerError
	case model.ErrCardTokenOwnerMismatch:
		return http.StatusForbidden
	case model.ErrTemporaryCardTokenBad:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
