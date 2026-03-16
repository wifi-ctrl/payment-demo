package http

import (
	"encoding/json"
	"net/http"

	"payment-demo/internal/catalog/application"
	"payment-demo/internal/catalog/domain/model"
)

// CatalogHandler HTTP 驱动适配器
type CatalogHandler struct {
	useCase *application.CatalogUseCase
}

func NewCatalogHandler(uc *application.CatalogUseCase) *CatalogHandler {
	return &CatalogHandler{useCase: uc}
}

func (h *CatalogHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/products", h.handleProducts)
}

type ProductResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Status   string `json:"status"`
}

func (h *CatalogHandler) handleProducts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id != "" {
		product, err := h.useCase.GetProduct(r.Context(), model.ProductID(id))
		if err != nil {
			jsonError(w, err.Error(), http.StatusNotFound)
			return
		}
		jsonOK(w, toProductResponse(product))
		return
	}

	products, err := h.useCase.ListProducts(r.Context())
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := make([]ProductResponse, 0, len(products))
	for _, p := range products {
		resp = append(resp, toProductResponse(p))
	}
	jsonOK(w, resp)
}

func toProductResponse(p *model.Product) ProductResponse {
	return ProductResponse{
		ID:       string(p.ID),
		Name:     p.Name,
		Amount:   p.Price.Amount,
		Currency: p.Price.Currency,
		Status:   string(p.Status),
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
