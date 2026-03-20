package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"payment-demo/internal/card/application"
	"payment-demo/internal/card/domain/model"
	"payment-demo/internal/shared/auth"
	"payment-demo/internal/shared/httputil"
)

// CardHandler HTTP 驱动适配器
type CardHandler struct {
	useCase         *application.CardUseCase
	tokenizeLimiter *rateLimiter // PCI Req 8: 令牌化接口限流
}

func NewCardHandler(uc *application.CardUseCase) *CardHandler {
	return &CardHandler{
		useCase:         uc,
		tokenizeLimiter: newRateLimiter(10, 1*time.Minute), // 每用户每分钟 10 次
	}
}

// RegisterRoutes 注册路由
//
// 绑卡（持久化 SavedCard）不暴露为独立 HTTP 端点 —— 只在 Capture 成功后由
// ChargeUseCase.postCaptureCardBinding 内部调用 CardCommand.BindCardFromToken，
// 确保只有经过网关验证 + 扣款成功的卡才能被持久化。
func (h *CardHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/cards", h.handleCards) // GET（列表/详情），DELETE（删卡）
	mux.HandleFunc("/cards/tokenize", rateLimitMiddleware(h.tokenizeLimiter, func(r *http.Request) string {
		if uid, ok := auth.UserIDFromContext(r.Context()); ok {
			return uid
		}
		return r.RemoteAddr
	}, h.handleTokenize)) // POST /cards/tokenize (rate limited)
	mux.HandleFunc("/cards/suspend", h.handleSuspend)    // POST /cards/suspend
	mux.HandleFunc("/cards/activate", h.handleActivate)  // POST /cards/activate
	mux.HandleFunc("/cards/default", h.handleSetDefault) // POST /cards/default
}

// --- Request / Response DTOs ---

// TokenizeRequest POST /cards/tokenize 请求体（pan + 有效期 + cvv 必填，由 handler 校验）
type TokenizeRequest struct {
	PAN            string `json:"pan"`
	ExpiryMonth    int    `json:"expiry_month"` // 1–12
	ExpiryYear     int    `json:"expiry_year"`  // 四位年或两位年（YY→20YY）
	CVV            string `json:"cvv"`          // 3 或 4 位数字
	CardholderName string `json:"cardholder_name"`
}

type TokenizeResponse struct {
	CardToken      string `json:"card_token,omitempty"`       // 临时 token（查重命中时为空）
	ExistingCardID string `json:"existing_card_id,omitempty"` // 查重命中时返回已存卡 ID
	Last4          string `json:"last4"`
	Brand          string `json:"brand"`
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

// handleCards 处理 GET /cards（列表或详情）、DELETE /cards（删卡）
// 绑卡不在此暴露 — 只在支付成功后由 ChargeUseCase 内部触发
func (h *CardHandler) handleCards(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if id := r.URL.Query().Get("id"); id != "" {
			h.handleGet(w, r, id)
		} else {
			h.handleList(w, r)
		}
	case http.MethodDelete:
		h.handleDelete(w, r)
	default:
		httputil.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Handlers ---

// GET /cards
func (h *CardHandler) handleList(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httputil.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	cards, err := h.useCase.ListCards(r.Context(), userID)
	if err != nil {
		httputil.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	resp := make([]CardResponse, 0, len(cards))
	for _, c := range cards {
		resp = append(resp, toResponse(c))
	}
	httputil.OK(w, resp)
}

// GET /cards?id=xxx
func (h *CardHandler) handleGet(w http.ResponseWriter, r *http.Request, cardID string) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httputil.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	card, err := h.useCase.GetCard(r.Context(), userID, model.SavedCardID(cardID))
	if err != nil {
		httputil.UseCaseError(w, err, mapErrorStatus)
		return
	}

	httputil.OK(w, toResponse(card))
}

// DELETE /cards with body {"card_id": "xxx"}
// AC-39 要求 200 OK + {"status":"DELETED"}
func (h *CardHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httputil.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req CardIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.CardID == "" {
		httputil.Error(w, "card_id is required", http.StatusBadRequest)
		return
	}

	if err := h.useCase.DeleteCard(r.Context(), userID, model.SavedCardID(req.CardID)); err != nil {
		httputil.UseCaseError(w, err, mapErrorStatus)
		return
	}

	// 返回 200 + 已删除的状态信息（AC-39 验收标准）
	httputil.OK(w, map[string]string{"status": string(model.CardStatusDeleted)})
}

// POST /cards/tokenize
func (h *CardHandler) handleTokenize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httputil.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req TokenizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.PAN == "" {
		httputil.Error(w, "pan is required", http.StatusBadRequest)
		return
	}
	if err := validateTokenizeRequest(req); err != nil {
		httputil.Error(w, err.Error(), http.StatusBadRequest)
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
		httputil.UseCaseError(w, err, mapErrorStatus)
		return
	}

	resp := TokenizeResponse{
		Brand: string(result.Brand),
		Last4: result.Mask.Last4,
	}
	if result.CardToken != nil {
		resp.CardToken = *result.CardToken
	}
	if result.ExistingCardID != nil {
		resp.ExistingCardID = string(*result.ExistingCardID)
	}

	httputil.Created(w, resp)
}

// POST /cards/suspend with body {"card_id": "xxx"}
func (h *CardHandler) handleSuspend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httputil.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req CardIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.CardID == "" {
		httputil.Error(w, "card_id is required", http.StatusBadRequest)
		return
	}

	card, err := h.useCase.SuspendCard(r.Context(), userID, model.SavedCardID(req.CardID))
	if err != nil {
		httputil.UseCaseError(w, err, mapErrorStatus)
		return
	}

	httputil.OK(w, toResponse(card))
}

// POST /cards/activate with body {"card_id": "xxx"}
func (h *CardHandler) handleActivate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httputil.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req CardIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.CardID == "" {
		httputil.Error(w, "card_id is required", http.StatusBadRequest)
		return
	}

	card, err := h.useCase.ActivateCard(r.Context(), userID, model.SavedCardID(req.CardID))
	if err != nil {
		httputil.UseCaseError(w, err, mapErrorStatus)
		return
	}

	httputil.OK(w, toResponse(card))
}

// POST /cards/default with body {"card_id": "xxx"} (也接受 PUT，兼容 AC-41)
func (h *CardHandler) handleSetDefault(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		httputil.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httputil.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req CardIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.CardID == "" {
		httputil.Error(w, "card_id is required", http.StatusBadRequest)
		return
	}

	card, err := h.useCase.SetDefaultCard(r.Context(), userID, model.SavedCardID(req.CardID))
	if err != nil {
		httputil.UseCaseError(w, err, mapErrorStatus)
		return
	}

	httputil.OK(w, toResponse(card))
}

// --- Helpers ---

func validateTokenizeRequest(req TokenizeRequest) error {
	if req.ExpiryMonth < 1 || req.ExpiryMonth > 12 {
		return errors.New("expiry_month must be between 1 and 12")
	}
	if req.ExpiryYear <= 0 {
		return errors.New("expiry_year is required")
	}
	year := req.ExpiryYear
	if year < 100 {
		year += 2000
	}
	if year < 2000 || year > 2100 {
		return errors.New("expiry_year is invalid")
	}
	now := time.Now().UTC()
	curY, curM := now.Year(), int(now.Month())
	if year < curY || (year == curY && req.ExpiryMonth < curM) {
		return errors.New("card is expired")
	}
	n := len(req.CVV)
	if n != 3 && n != 4 {
		return errors.New("cvv must be 3 or 4 digits")
	}
	for _, c := range req.CVV {
		if c < '0' || c > '9' {
			return errors.New("cvv must contain only digits")
		}
	}
	return nil
}

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
