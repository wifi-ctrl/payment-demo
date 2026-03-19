package bootstrap

import (
	merchantPersistence "payment-demo/internal/merchant/adapter/persistence"
	merchantApp "payment-demo/internal/merchant/application"
	merchantHTTP "payment-demo/internal/merchant/handler/http"
)

// MerchantModule merchant 上下文的组装产物。
// MerchantRepo 共享给 payment ACL adapter（MerchantAdapter）。
type MerchantModule struct {
	Handler      *merchantHTTP.MerchantHandler
	MerchantRepo *merchantPersistence.InMemoryMerchantRepository
}

func wireMerchant() *MerchantModule {
	repo := merchantPersistence.NewInMemoryMerchantRepository()
	uc := merchantApp.NewMerchantUseCase(repo)
	return &MerchantModule{
		Handler:      merchantHTTP.NewMerchantHandler(uc),
		MerchantRepo: repo,
	}
}
