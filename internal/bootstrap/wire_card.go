package bootstrap

import (
	cardPersistence "payment-demo/internal/card/adapter/persistence"
	cardVault "payment-demo/internal/card/adapter/vault"
	cardApp "payment-demo/internal/card/application"
	cardHTTP "payment-demo/internal/card/handler/http"
	"payment-demo/internal/infra/stripe"
)

// CardModule card 上下文的组装产物。
// CardRepo 共享给 payment ACL adapter（CardAdapter）。
type CardModule struct {
	Handler  *cardHTTP.CardHandler
	CardRepo *cardPersistence.InMemoryCardRepository
}

// wireCard 组装 card 上下文。
// 接收共享的 stripe.Client，用于构造 StripeVaultAdapter。
func wireCard(stripeClient *stripe.Client) *CardModule {
	repo := cardPersistence.NewInMemoryCardRepository()
	vault := cardVault.NewStripeVaultAdapter(stripeClient)
	uc := cardApp.NewCardUseCase(repo, vault)
	return &CardModule{
		Handler:  cardHTTP.NewCardHandler(uc),
		CardRepo: repo,
	}
}
