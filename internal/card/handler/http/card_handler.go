package http

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"payment-demo/internal/card/application"
	"payment-demo/internal/shared/auth"
	"payment-demo/internal/card/domain/model"
)

// CardHandler HTTP 驱动适配器
type CardHandler struct {
	useCase *application.CardUseCase
}

func NewCardHandler(uc *application.CardUseCase) *CardHandler {
	return &CardHandler{useCase: uc}
}

// RegisterRoutes 注册路由
func (h *CardHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/cards", h.handleCards)              // POST（绑卡），GET（列表/详情），DELETE（删卡）
	mux.HandleFunc("/cards/tokenize", h.handleTokenize)  // POST /cards/tokenize
	mux.HandleFunc("/cards/suspend", h.handleSuspend)    // POST /cards/suspend
	mux.HandleFunc("/cards/activate", h.handleActivate)  // POST /cards/activate
	mux.HandleFunc("/cards/default", h.handleSetDefault) // POST /cards/default
}

// --- Request / Response DTOs ---

type TokenizeRequest struct {
	PAN            string `json:"pan"`
	ExpiryMonth    int    `json:"expiry_month"`
	ExpiryYear     int    `json:"expiry_year"`
	CVV            string `json:"cvv"`
	CardholderName string `json:"cardholder_name"`
}

type TokenizeResponse struct {
	CardToken string `json:"card_token"`
	Last4     string `json:"last4"`
	Brand     string `json:"brand"`
}

type BindCardRequest struct {
	OneTimeToken string `json:"one_time_token"`
}

// CardIDRequest 用于需要 card_id 的请求体
type CardIDRequest struct {
	CardID string `json:"card_id"`
}

// CardResponse 卡详情响应体（含 card_id 别名，方便 AC-35 校验）
type CardResponse struct {
	ID          string `json:"id"`
	CardID      string `json:"card_id"` // AC-35 要求响应包含 card_id 字段
	UserID      string `json:"user_id"`
	Last4       string `json:"last4"`
	Brand       string `json:"brand"`
	ExpireMonth int    `json:"expire_month"`
	ExpireYear  int    `json:"expire_year"`
	Holder      string `json:"holder_name"`
	Country     string `json:"billing_country"`
	IsDefault   bool   `json:"is_default"`
	Status      string `json:"status"`
}

// --- Route Dispatcher ---

// handleCards 处理 POST /cards（绑卡）、GET /cards（列表或详情）、DELETE /cards（删卡）
func (h *CardHandler) handleCards(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleBind(w, r)
	case http.MethodGet:
		// 如果有 id 查询参数，返回单张卡详情；否则返回列表
		if id := r.URL.Query().Get("id"); id != "" {
			h.handleGet(w, r, id)
		} else {
			h.handleList(w, r)
		}
	case http.MethodDelete:
		h.handleDelete(w, r)
	default:
		// W-4: 统一使用 jsonError 保证错误响应格式一致
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Handlers ---

// POST /cards — 绑卡（需先调用 POST /cards/tokenize 获取 card_token）
func (h *CardHandler) handleBind(w http.ResponseWriter, r *http.Request) {
	var req BindCardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.OneTimeToken == "" {
		jsonError(w, "one_time_token is required", http.StatusBadRequest)
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	_ = userID // card_token 中已含 userID

	card, err := h.useCase.BindCardFromToken(r.Context(), application.BindFromTokenRequest{
		CardToken: req.OneTimeToken,
	})
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	w.WriteHeader(http.StatusCreated)
	jsonOK(w, toResponse(card))
}

// GET /cards
func (h *CardHandler) handleList(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	cards, err := h.useCase.ListCards(r.Context(), userID)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := make([]CardResponse, 0, len(cards))
	for _, c := range cards {
		resp = append(resp, toResponse(c))
	}
	jsonOK(w, resp)
}

// GET /cards?id=xxx
func (h *CardHandler) handleGet(w http.ResponseWriter, r *http.Request, cardID string) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	card, err := h.useCase.GetCard(r.Context(), userID, model.SavedCardID(cardID))
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	jsonOK(w, toResponse(card))
}

// DELETE /cards with body {"card_id": "xxx"}
// AC-39 要求 200 OK + {"status":"DELETED"}
func (h *CardHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req CardIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.CardID == "" {
		jsonError(w, "card_id is required", http.StatusBadRequest)
		return
	}

	if err := h.useCase.DeleteCard(r.Context(), userID, model.SavedCardID(req.CardID)); err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	// 返回 200 + 已删除的状态信息（AC-39 验收标准）
	jsonOK(w, map[string]string{"status": string(model.CardStatusDeleted)})
}

// POST /cards/tokenize
func (h *CardHandler) handleTokenize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req TokenizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.PAN == "" {
		jsonError(w, "pan is required", http.StatusBadRequest)
		return
	}

	result, err := h.useCase.Tokenize(r.Context(), application.TokenizeRequest{
		UserID:         userID,
		PAN:            req.PAN,
		ExpiryMonth:    req.ExpiryMonth,
		ExpiryYear:     req.ExpiryYear,
		CVV:            req.CVV,
		CardholderName: req.CardholderName,
	})
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	resp := TokenizeResponse{
		Brand: string(result.Brand),
		Last4: result.Mask.Last4,
	}
	if result.CardToken != nil {
		resp.CardToken = *result.CardToken
	}

	w.WriteHeader(http.StatusCreated)
	jsonOK(w, resp)
}

// POST /cards/suspend with body {"card_id": "xxx"}
func (h *CardHandler) handleSuspend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req CardIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.CardID == "" {
		jsonError(w, "card_id is required", http.StatusBadRequest)
		return
	}

	card, err := h.useCase.SuspendCard(r.Context(), userID, model.SavedCardID(req.CardID))
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	jsonOK(w, toResponse(card))
}

// POST /cards/activate with body {"card_id": "xxx"}
func (h *CardHandler) handleActivate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req CardIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.CardID == "" {
		jsonError(w, "card_id is required", http.StatusBadRequest)
		return
	}

	card, err := h.useCase.ActivateCard(r.Context(), userID, model.SavedCardID(req.CardID))
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	jsonOK(w, toResponse(card))
}

// POST /cards/default with body {"card_id": "xxx"} (也接受 PUT，兼容 AC-41)
func (h *CardHandler) handleSetDefault(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req CardIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.CardID == "" {
		jsonError(w, "card_id is required", http.StatusBadRequest)
		return
	}

	card, err := h.useCase.SetDefaultCard(r.Context(), userID, model.SavedCardID(req.CardID))
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	jsonOK(w, toResponse(card))
}

// --- Helpers ---

func toResponse(card *model.SavedCard) CardResponse {
	id := string(card.ID)
	return CardResponse{
		ID:          id,
		CardID:      id, // AC-35 要求响应体包含 card_id
		UserID:      card.UserID,
		Last4:       card.Mask.Last4,
		Brand:       card.Mask.Brand,
		ExpireMonth: card.Mask.ExpireMonth,
		ExpireYear:  card.Mask.ExpireYear,
		Holder:      card.Holder.Name,
		Country:     card.Holder.BillingCountry,
		IsDefault:   card.IsDefault,
		Status:      string(card.Status),
	}
}

// mapErrorStatus 将领域错误映射为 HTTP 状态码。
// R-2: 使用 errors.Is() 支持被 fmt.Errorf("%w", ...) 包装过的错误。
func mapErrorStatus(err error) int {
	switch {
	case errors.Is(err, model.ErrCardNotFound):
		return http.StatusNotFound
	case errors.Is(err, model.ErrInvalidStateTransition):
		return http.StatusConflict
	case errors.Is(err, model.ErrCardNotUsable):
		return http.StatusUnprocessableEntity
	case errors.Is(err, model.ErrCardBelongsToOtherUser):
		return http.StatusForbidden
	case errors.Is(err, model.ErrVaultTokenizeFailed):
		return http.StatusBadGateway
	case errors.Is(err, model.ErrDuplicateCard):
		return http.StatusConflict
	case errors.Is(err, model.ErrEncryptionFailed),
		errors.Is(err, model.ErrDecryptionFailed):
		return http.StatusInternalServerError
	case errors.Is(err, model.ErrCardTokenExpired),
		errors.Is(err, model.ErrCardTokenInvalid):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func jsonOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("[CardHandler] jsonOK encode error: %v", err)
	}
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		log.Printf("[CardHandler] jsonError encode error: %v", err)
	}
}
