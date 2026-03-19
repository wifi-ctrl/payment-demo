package bootstrap

import (
	catalogPersistence "payment-demo/internal/catalog/adapter/persistence"
	catalogApp "payment-demo/internal/catalog/application"
	catalogHTTP "payment-demo/internal/catalog/handler/http"
)

// CatalogModule catalog 上下文的组装产物。
// ProductRepo 共享给 payment ACL adapter（CatalogAdapter）。
type CatalogModule struct {
	Handler     *catalogHTTP.CatalogHandler
	ProductRepo *catalogPersistence.InMemoryProductRepository
}

func wireCatalog() *CatalogModule {
	repo := catalogPersistence.NewInMemoryProductRepository()
	uc := catalogApp.NewCatalogUseCase(repo)
	return &CatalogModule{
		Handler:     catalogHTTP.NewCatalogHandler(uc),
		ProductRepo: repo,
	}
}
