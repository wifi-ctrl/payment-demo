package bootstrap

import (
	cardCrypto "payment-demo/internal/card/adapter/crypto"
	cardKeyMgr "payment-demo/internal/card/adapter/keymanager"
	cardPersistence "payment-demo/internal/card/adapter/persistence"
	cardVault "payment-demo/internal/card/adapter/vault"
	cardApp "payment-demo/internal/card/application"
	cardService "payment-demo/internal/card/domain/service"
	cardHTTP "payment-demo/internal/card/handler/http"
)

// CardModule card 上下文的组装产物。
// CardRepo / CardUseCase 共享给 payment ACL adapter。
type CardModule struct {
	Handler  *cardHTTP.CardHandler
	CardRepo *cardPersistence.InMemoryCardRepository
	CardUC   *cardApp.CardUseCase
}

// wireCard 组装 card 上下文。
func wireCard() *CardModule {
	repo := cardPersistence.NewInMemoryCardRepository()

	// 加密基础设施
	keyMgr := cardKeyMgr.NewInMemKeyManager()
	encrypter := cardCrypto.NewAESEncrypter()
	encSvc := cardService.NewEncryptionService(keyMgr, encrypter)

	// 本地 Vault（替换旧 StripeVaultAdapter）
	vault := cardVault.NewLocalVault()

	uc := cardApp.NewCardUseCase(repo, vault, encSvc)
	return &CardModule{
		Handler:  cardHTTP.NewCardHandler(uc),
		CardRepo: repo,
		CardUC:   uc,
	}
}
