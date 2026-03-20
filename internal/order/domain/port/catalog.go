package port

import "context"

type ProductView struct {
	ID       string
	Name     string
	Amount   int64
	Currency string
	IsActive bool
}

type CatalogQuery interface {
	FindProduct(ctx context.Context, productID string) (*ProductView, error)
}
