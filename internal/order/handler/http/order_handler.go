package http

import (
	"encoding/json"
	"net/http"
	"strings"


	"payment-demo/internal/order/application"
	"payment-demo/internal/order/domain/model"
	"payment-demo/internal/shared/auth"
	"payment-demo/internal/shared/httputil"
)

type OrderHandler struct {
	useCase *application.OrderUseCase
}

func NewOrderHandler(uc *application.OrderUseCase) *OrderHandler {
	return &OrderHandler{useCase: uc}
}

func (h *OrderHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/orders", h.handleCreateOrder)
	mux.HandleFunc("/orders/", h.handleOrderAction)
}

// ── Request / Response DTOs ────────────────────────────────────

type CreateOrderRequest struct {
	MerchantID    string `json:"merchant_id"`
	ProductID     string `json:"product_id"`
	CouponCode    string `json:"coupon_code,omitempty"`
	PaymentMethod string `json:"payment_method"` // "CARD" | "PAYPAL"
	TokenID       string `json:"token_id,omitempty"`
	Last4         string `json:"last4,omitempty"`
	Brand         string `json:"brand,omitempty"`
	SavedCardID   string `json:"saved_card_id,omitempty"`
	SaveCard      bool   `json:"save_card,omitempty"`
	PayPalOrderID string `json:"paypal_order_id,omitempty"`
	PayPalPayerID string `json:"paypal_payer_id,omitempty"`
}

type OrderResponse struct {
	ID             string `json:"id"`
	UserID         string `json:"user_id"`
	MerchantID     string `json:"merchant_id"`
	ProductID      string `json:"product_id"`
	ProductName    string `json:"product_name"`
	Status         string `json:"status"`
	OriginalAmount int64  `json:"original_amount"`
	DiscountAmount int64  `json:"discount_amount,omitempty"`
	TaxAmount      int64  `json:"tax_amount,omitempty"`
	FinalAmount    int64  `json:"final_amount"`
	Currency       string `json:"currency"`
	CouponID       string `json:"coupon_id,omitempty"`
	TransactionID  string `json:"transaction_id,omitempty"`
}

// ── Handlers ───────────────────────────────────────────────────

func (h *OrderHandler) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.MerchantID == "" {
		httputil.Error(w, "merchant_id is required", http.StatusBadRequest)
		return
	}
	if req.ProductID == "" {
		httputil.Error(w, "product_id is required", http.StatusBadRequest)
		return
	}
	if req.PaymentMethod == "" {
		req.PaymentMethod = "CARD"
	}
	if req.PaymentMethod == "CARD" && req.SavedCardID == "" && req.TokenID == "" {
		httputil.Error(w, "saved_card_id or token_id is required for CARD payment", http.StatusBadRequest)
		return
	}
	if req.PaymentMethod == "PAYPAL" && (req.PayPalOrderID == "" || req.PayPalPayerID == "") {
		httputil.Error(w, "paypal_order_id and paypal_payer_id are required for PAYPAL payment", http.StatusBadRequest)
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httputil.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	order, err := h.useCase.CreateOrder(r.Context(), application.CreateOrderRequest{
		MerchantID: req.MerchantID,
		UserID:     userID,
		ProductID:  req.ProductID,
		CouponCode: req.CouponCode,
	}, application.PaymentDetail{
		CardToken:     req.TokenID,
		CardLast4:     req.Last4,
		CardBrand:     req.Brand,
		SavedCardID:   req.SavedCardID,
		SaveCard:      req.SaveCard,
		PaymentMethod: req.PaymentMethod,
		PayPalOrderID: req.PayPalOrderID,
		PayPalPayerID: req.PayPalPayerID,
	})
	if err != nil {
		httputil.UseCaseError(w, err, mapErrorStatus)
		return
	}
	httputil.OK(w, toResponse(order))
}

func (h *OrderHandler) handleOrderAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/orders/")
	parts := strings.SplitN(path, "/", 2)
	orderID := parts[0]
	if orderID == "" {
		httputil.Error(w, "order id is required", http.StatusBadRequest)
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httputil.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			httputil.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		order, err := h.useCase.GetOrder(r.Context(), userID, model.OrderID(orderID))
		if err != nil {
			httputil.UseCaseError(w, err, mapErrorStatus)
			return
		}
		httputil.OK(w, toResponse(order))
		return
	}

	action := parts[1]
	if r.Method != http.MethodPost {
		httputil.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch action {
	case "capture":
		order, err := h.useCase.CaptureOrder(r.Context(), userID, model.OrderID(orderID))
		if err != nil {
			httputil.UseCaseError(w, err, mapErrorStatus)
			return
		}
		httputil.OK(w, toResponse(order))
	case "refund":
		order, err := h.useCase.RefundOrder(r.Context(), userID, model.OrderID(orderID))
		if err != nil {
			httputil.UseCaseError(w, err, mapErrorStatus)
			return
		}
		httputil.OK(w, toResponse(order))
	default:
		httputil.Error(w, "unknown action", http.StatusNotFound)
	}
}

// ── Helpers ────────────────────────────────────────────────────

func toResponse(o *model.Order) OrderResponse {
	return OrderResponse{
		ID:             string(o.ID),
		UserID:         o.UserID,
		MerchantID:     o.MerchantID,
		ProductID:      o.ProductID,
		ProductName:    o.ProductName,
		Status:         string(o.Status),
		OriginalAmount: o.Price.OriginalAmount.Amount,
		DiscountAmount: o.Price.DiscountAmount.Amount,
		TaxAmount:      o.Price.TaxAmount.Amount,
		FinalAmount:    o.Price.FinalAmount.Amount,
		Currency:       o.Price.FinalAmount.Currency,
		CouponID:       o.CouponID,
		TransactionID:  o.TransactionID,
	}
}

func mapErrorStatus(err error) int {
	switch err {
	case model.ErrOrderNotFound:
		return http.StatusNotFound
	case model.ErrInvalidStateTransition:
		return http.StatusConflict
	case model.ErrProductNotFound:
		return http.StatusNotFound
	case model.ErrProductNotActive, model.ErrMerchantRequired:
		return http.StatusBadRequest
	case model.ErrPaymentFailed:
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}
