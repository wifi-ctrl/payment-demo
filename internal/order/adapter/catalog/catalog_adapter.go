package catalog

import (
	"context"

	catalogModel "payment-demo/internal/catalog/domain/model"
	catalogPort "payment-demo/internal/catalog/domain/port"
	"payment-demo/internal/order/domain/model"
	"payment-demo/internal/order/domain/port"
)

type CatalogAdapter struct {
	repo catalogPort.ProductRepository
}

var _ port.CatalogQuery = (*CatalogAdapter)(nil)

func NewCatalogAdapter(repo catalogPort.ProductRepository) *CatalogAdapter {
	return &CatalogAdapter{repo: repo}
}

func (a *CatalogAdapter) FindProduct(ctx context.Context, productID string) (*port.ProductView, error) {
	product, err := a.repo.FindByID(ctx, catalogModel.ProductID(productID))
	if err != nil {
		return nil, model.ErrProductNotFound
	}
	return &port.ProductView{
		ID:       string(product.ID),
		Name:     product.Name,
		Amount:   product.Price.Amount,
		Currency: product.Price.Currency,
		IsActive: product.IsActive(),
	}, nil
}
