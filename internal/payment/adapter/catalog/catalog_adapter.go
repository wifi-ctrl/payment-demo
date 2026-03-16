package catalog

import (
	"context"

	catalogModel "payment-demo/internal/catalog/domain/model"
	catalogPort "payment-demo/internal/catalog/domain/port"
	"payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// CatalogAdapter ACL 适配器
// 实现 payment 的 CatalogQuery 端口，内部调用 catalog 上下文的 ProductRepository
// 翻译：catalog.Product → payment.ProductView
type CatalogAdapter struct {
	repo catalogPort.ProductRepository
}

var _ port.CatalogQuery = (*CatalogAdapter)(nil)

func NewCatalogAdapter(repo catalogPort.ProductRepository) *CatalogAdapter {
	return &CatalogAdapter{repo: repo}
}

func (a *CatalogAdapter) FindProduct(ctx context.Context, productID string) (*port.ProductView, error) {
	// 调用 catalog 上下文的仓储
	product, err := a.repo.FindByID(ctx, catalogModel.ProductID(productID))
	if err != nil {
		return nil, model.ErrProductNotFound
	}

	// ACL 翻译：catalog 的丰富模型 → payment 的极简视图
	return &port.ProductView{
		ID:       string(product.ID),
		Name:     product.Name,
		Amount:   product.Price.Amount,
		Currency: product.Price.Currency,
		IsActive: product.IsActive(),
	}, nil
}
