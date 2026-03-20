// Package http 提供 acquiring 上下文的 HTTP 驱动适配器。
package http

import (
	"encoding/json"
	"net/http"

	"payment-demo/internal/acquiring/application"
	"payment-demo/internal/acquiring/domain/model"
	"payment-demo/internal/shared/httputil"
)

// MerchantHandler merchant 管理 HTTP handler。
type MerchantHandler struct {
	useCase *application.MerchantUseCase
}

// NewMerchantHandler 构造函数。
func NewMerchantHandler(uc *application.MerchantUseCase) *MerchantHandler {
	return &MerchantHandler{useCase: uc}
}

// RegisterRoutes 注册路由：
//
//	POST   /merchants              — 注册商户
//	GET    /merchants              — 列出所有商户（无 id 参数）或查询商户详情（?id=xxx）
//	POST   /merchants/credentials  — 添加渠道凭据（merchant_id 在 body 中）
//	DELETE /merchants/credentials  — 吊销渠道凭据（merchant_id + credential_id 在 body 中）
//	POST   /merchants/suspend      — 暂停商户（merchant_id 在 body 中）
func (h *MerchantHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/merchants", h.handleMerchants)
	mux.HandleFunc("/merchants/credentials", h.handleCredentials)
	mux.HandleFunc("/merchants/suspend", h.handleSuspend)
}

// ─────────────────────────────────────────────────────────────────
// Request / Response DTOs
// ─────────────────────────────────────────────────────────────────

// RegisterMerchantRequest 注册商户请求。
type RegisterMerchantRequest struct {
	Name string `json:"name"`
}

// AddCredentialRequest 添加渠道凭据请求。
// channel 取值 "CARD" 或 "PAYPAL"（与 PaymentMethod 对齐）。
// secrets 内容因渠道而异：
//   - CARD:   {"api_key": "sk_live_xxx"}
//   - PAYPAL: {"client_id": "...", "client_secret": "..."}
type AddCredentialRequest struct {
	MerchantID string            `json:"merchant_id"`
	Channel    string            `json:"channel"`
	Secrets    map[string]string `json:"secrets"`
}

// RevokeCredentialRequest 吊销凭据请求。
type RevokeCredentialRequest struct {
	MerchantID   string `json:"merchant_id"`
	CredentialID string `json:"credential_id"`
}

// SuspendRequest 暂停商户请求。
type SuspendRequest struct {
	MerchantID string `json:"merchant_id"`
}

// CredentialResponse 凭据视图响应（不含 Secrets，安全考虑）。
type CredentialResponse struct {
	ID        string `json:"id"`
	Channel   string `json:"channel"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// MerchantResponse 商户响应。
type MerchantResponse struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Status      string               `json:"status"`
	Credentials []CredentialResponse `json:"credentials"`
	CreatedAt   string               `json:"created_at"`
}

// ─────────────────────────────────────────────────────────────────
// 路由分派
// ─────────────────────────────────────────────────────────────────

// handleMerchants 处理 /merchants
//   - POST → 注册商户
//   - GET  → 如果有 ?id=xxx 查询详情，否则列出所有商户
func (h *MerchantHandler) handleMerchants(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleRegister(w, r)
	case http.MethodGet:
		id := r.URL.Query().Get("id")
		if id != "" {
			h.handleGetMerchant(w, r, id)
		} else {
			h.handleListMerchants(w, r)
		}
	default:
		httputil.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCredentials 处理 /merchants/credentials
//   - POST   → 添加渠道凭据
//   - DELETE → 吊销渠道凭据
func (h *MerchantHandler) handleCredentials(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleAddCredential(w, r)
	case http.MethodDelete:
		h.handleRevokeCredential(w, r)
	default:
		httputil.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSuspend 处理 POST /merchants/suspend — 暂停商户
func (h *MerchantHandler) handleSuspend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SuspendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.MerchantID == "" {
		httputil.Error(w, "merchant_id is required", http.StatusBadRequest)
		return
	}

	m, err := h.useCase.Suspend(r.Context(), req.MerchantID)
	if err != nil {
		httputil.UseCaseError(w, err, mapMerchantErrorStatus)
		return
	}
	httputil.OK(w, toMerchantResponse(m))
}

// ─────────────────────────────────────────────────────────────────
// 具体 Handler 实现
// ─────────────────────────────────────────────────────────────────

// handleRegister POST /merchants — 注册新商户
func (h *MerchantHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterMerchantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		httputil.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	m, err := h.useCase.Register(r.Context(), application.RegisterRequest{Name: req.Name})
	if err != nil {
		httputil.UseCaseError(w, err, mapMerchantErrorStatus)
		return
	}

	httputil.Created(w, toMerchantResponse(m))
}

// handleListMerchants GET /merchants — 列出所有商户
func (h *MerchantHandler) handleListMerchants(w http.ResponseWriter, r *http.Request) {
	merchants, err := h.useCase.ListMerchants(r.Context())
	if err != nil {
		httputil.UseCaseError(w, err, mapMerchantErrorStatus)
		return
	}

	resp := make([]MerchantResponse, 0, len(merchants))
	for _, m := range merchants {
		resp = append(resp, toMerchantResponse(m))
	}
	httputil.OK(w, resp)
}

// handleGetMerchant GET /merchants?id=xxx — 查询商户详情
func (h *MerchantHandler) handleGetMerchant(w http.ResponseWriter, r *http.Request, merchantID string) {
	m, err := h.useCase.GetMerchant(r.Context(), merchantID)
	if err != nil {
		httputil.UseCaseError(w, err, mapMerchantErrorStatus)
		return
	}
	httputil.OK(w, toMerchantResponse(m))
}

// handleAddCredential POST /merchants/credentials — 添加渠道凭据
func (h *MerchantHandler) handleAddCredential(w http.ResponseWriter, r *http.Request) {
	var req AddCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.MerchantID == "" {
		httputil.Error(w, "merchant_id is required", http.StatusBadRequest)
		return
	}
	if req.Channel == "" {
		httputil.Error(w, "channel is required", http.StatusBadRequest)
		return
	}
	if len(req.Secrets) == 0 {
		httputil.Error(w, "secrets are required", http.StatusBadRequest)
		return
	}

	m, err := h.useCase.AddCredential(r.Context(), application.AddCredentialRequest{
		MerchantID: req.MerchantID,
		Channel:    model.PaymentMethod(req.Channel),
		Secrets:    req.Secrets,
	})
	if err != nil {
		httputil.UseCaseError(w, err, mapMerchantErrorStatus)
		return
	}

	httputil.Created(w, toMerchantResponse(m))
}

// handleRevokeCredential DELETE /merchants/credentials — 吊销凭据
func (h *MerchantHandler) handleRevokeCredential(w http.ResponseWriter, r *http.Request) {
	var req RevokeCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.MerchantID == "" {
		httputil.Error(w, "merchant_id is required", http.StatusBadRequest)
		return
	}
	if req.CredentialID == "" {
		httputil.Error(w, "credential_id is required", http.StatusBadRequest)
		return
	}

	m, err := h.useCase.RevokeCredential(r.Context(), application.RevokeCredentialRequest{
		MerchantID:   req.MerchantID,
		CredentialID: req.CredentialID,
	})
	if err != nil {
		httputil.UseCaseError(w, err, mapMerchantErrorStatus)
		return
	}
	httputil.OK(w, toMerchantResponse(m))
}

// ─────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────

func toMerchantResponse(m *model.Merchant) MerchantResponse {
	creds := make([]CredentialResponse, 0, len(m.Credentials))
	for _, c := range m.Credentials {
		creds = append(creds, CredentialResponse{
			ID:        string(c.ID),
			Channel:   string(c.Channel),
			Status:    string(c.Status),
			CreatedAt: c.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return MerchantResponse{
		ID:          string(m.ID),
		Name:        m.Name,
		Status:      string(m.Status),
		Credentials: creds,
		CreatedAt:   m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func mapMerchantErrorStatus(err error) int {
	switch err {
	case model.ErrMerchantNotFound, model.ErrCredentialNotFound:
		return http.StatusNotFound
	case model.ErrMerchantNotActive:
		return http.StatusBadRequest
	case model.ErrCredentialAlreadyExists:
		return http.StatusConflict
	case model.ErrInvalidStateTransition:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
