package application

import (
	"context"

	"payment-demo/internal/catalog/domain/model"
	"payment-demo/internal/catalog/domain/port"
)

// CatalogUseCase 商品目录用例
type CatalogUseCase struct {
	products port.ProductRepository
}

func NewCatalogUseCase(products port.ProductRepository) *CatalogUseCase {
	return &CatalogUseCase{products: products}
}

// GetProduct 查询单个商品
func (uc *CatalogUseCase) GetProduct(ctx context.Context, id model.ProductID) (*model.Product, error) {
	return uc.products.FindByID(ctx, id)
}

// ListProducts 列出所有商品
func (uc *CatalogUseCase) ListProducts(ctx context.Context) ([]*model.Product, error) {
	return uc.products.FindAll(ctx)
}
