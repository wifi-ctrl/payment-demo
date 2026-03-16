package http

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"payment-demo/internal/card/adapter/auth"
	"payment-demo/internal/card/application"
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
	mux.HandleFunc("/cards", h.handleCards)     // POST /cards（绑卡），GET /cards（列表）
	mux.HandleFunc("/cards/", h.handleCardByID) // GET/DELETE /cards/{id}，子路由见下
}

// --- Request / Response DTOs ---

type BindCardRequest struct {
	OneTimeToken string `json:"one_time_token"`
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

// handleCards 处理 POST /cards（绑卡）和 GET /cards（列表）
func (h *CardHandler) handleCards(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleBind(w, r)
	case http.MethodGet:
		h.handleList(w, r)
	default:
		// W-4: 统一使用 jsonError 保证错误响应格式一致
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCardByID 按子路径分发：
//
//	DELETE /cards/{id}          → 删除（200 + body）
//	POST   /cards/{id}/suspend  → 挂起
//	POST   /cards/{id}/activate → 激活
//	POST   /cards/{id}/default  → 设为默认（PUT 亦可）
//	GET    /cards/{id}          → 查详情
func (h *CardHandler) handleCardByID(w http.ResponseWriter, r *http.Request) {
	// 路径形如 /cards/{id} 或 /cards/{id}/suspend
	path := strings.TrimPrefix(r.URL.Path, "/cards/")
	parts := strings.SplitN(path, "/", 2)
	cardID := parts[0]
	if cardID == "" {
		// W-4: 统一使用 jsonError
		jsonError(w, "missing card id", http.StatusBadRequest)
		return
	}

	if len(parts) == 2 {
		// 有子路径
		switch parts[1] {
		case "suspend":
			if r.Method != http.MethodPost {
				// W-4: 统一使用 jsonError
				jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			h.handleSuspend(w, r, cardID)
		case "activate":
			if r.Method != http.MethodPost {
				// W-4: 统一使用 jsonError
				jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			h.handleActivate(w, r, cardID)
		case "default":
			// AC-41 用 PUT，兼容 POST
			if r.Method != http.MethodPost && r.Method != http.MethodPut {
				// W-4: 统一使用 jsonError
				jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			h.handleSetDefault(w, r, cardID)
		default:
			// W-4: 统一使用 jsonError
			jsonError(w, "not found", http.StatusNotFound)
		}
		return
	}

	// 无子路径：GET 查详情，DELETE 删除
	switch r.Method {
	case http.MethodGet:
		h.handleGet(w, r, cardID)
	case http.MethodDelete:
		h.handleDelete(w, r, cardID)
	default:
		// W-4: 统一使用 jsonError
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Handlers ---

// POST /cards
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

	// R-1: 通过 card/adapter/auth 读取 userID，消除 handler 层对 identity 包的直接依赖
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	card, err := h.useCase.BindCard(r.Context(), application.BindCardRequest{
		UserID:       userID,
		OneTimeToken: req.OneTimeToken,
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

// GET /cards/{id}
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

// DELETE /cards/{id}
// AC-39 要求 200 OK + {"status":"DELETED"}
func (h *CardHandler) handleDelete(w http.ResponseWriter, r *http.Request, cardID string) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.useCase.DeleteCard(r.Context(), userID, model.SavedCardID(cardID)); err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	// 返回 200 + 已删除的状态信息（AC-39 验收标准）
	jsonOK(w, map[string]string{"status": string(model.CardStatusDeleted)})
}

// POST /cards/{id}/suspend
func (h *CardHandler) handleSuspend(w http.ResponseWriter, r *http.Request, cardID string) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	card, err := h.useCase.SuspendCard(r.Context(), userID, model.SavedCardID(cardID))
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	jsonOK(w, toResponse(card))
}

// POST /cards/{id}/activate
func (h *CardHandler) handleActivate(w http.ResponseWriter, r *http.Request, cardID string) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	card, err := h.useCase.ActivateCard(r.Context(), userID, model.SavedCardID(cardID))
	if err != nil {
		jsonError(w, err.Error(), mapErrorStatus(err))
		return
	}

	jsonOK(w, toResponse(card))
}

// POST /cards/{id}/default  (也接受 PUT，兼容 AC-41 用 PUT 的场景)
func (h *CardHandler) handleSetDefault(w http.ResponseWriter, r *http.Request, cardID string) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	card, err := h.useCase.SetDefaultCard(r.Context(), userID, model.SavedCardID(cardID))
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
