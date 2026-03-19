package bootstrap

import (
	identityPersistence "payment-demo/internal/identity/adapter/persistence"
	identityApp "payment-demo/internal/identity/application"
	identityMW "payment-demo/internal/identity/handler/middleware"
)

// IdentityModule identity 上下文的组装产物。
// 不暴露 handler（identity 不注册路由），只暴露 Middleware 供全局包裹。
type IdentityModule struct {
	Middleware *identityMW.AuthMiddleware
}

func wireIdentity() *IdentityModule {
	userRepo := identityPersistence.NewInMemoryUserRepository()
	sessionRepo := identityPersistence.NewInMemorySessionRepository()
	uc := identityApp.NewAuthUseCase(userRepo, sessionRepo)
	return &IdentityModule{
		Middleware: identityMW.NewAuthMiddleware(uc),
	}
}
