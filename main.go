package main

import (
	"log"
	"net/http"

	// ── Identity Context ──
	identityPersistence "payment-demo/internal/identity/adapter/persistence"
	identityApp "payment-demo/internal/identity/application"
	identityMW "payment-demo/internal/identity/handler/middleware"

	// ── Catalog Context ──
	catalogPersistence "payment-demo/internal/catalog/adapter/persistence"
	catalogApp "payment-demo/internal/catalog/application"
	catalogHTTP "payment-demo/internal/catalog/handler/http"

	// ── Card Context ──
	// cardRepo 同时被 card usecase 和 payment 侧 ACL adapter 持有
	// 跨上下文共享 Repository 指针只允许在 main.go（Composition Root）进行
	cardPersistence "payment-demo/internal/card/adapter/persistence"
	cardVault "payment-demo/internal/card/adapter/vault"
	cardApp "payment-demo/internal/card/application"
	cardHTTP "payment-demo/internal/card/handler/http"

	// ── Payment Context ──
	// paymentCard / paymentCatalog 均为 ACL adapter，
	// 实现 payment 侧消费方定义的 port 接口，隔离跨上下文依赖
	paymentCard "payment-demo/internal/payment/adapter/card"
	paymentCatalog "payment-demo/internal/payment/adapter/catalog"
	paymentGateway "payment-demo/internal/payment/adapter/gateway"
	paymentPersistence "payment-demo/internal/payment/adapter/persistence"
	paymentApp "payment-demo/internal/payment/application"
	paymentHTTP "payment-demo/internal/payment/handler/http"

	// ── Shared Infra ──
	"payment-demo/internal/infra/config"
)

func main() {
	cfg := config.Load()

	// ========================================
	// Composition Root — 组装所有上下文
	// 这里是唯一知道所有上下文存在的地方
	// 组装顺序：adapter（persistence/gateway）→ usecase → handler
	// ========================================

	// ── Identity Context ──
	// userRepo / sessionRepo 仅在 identity 内部使用
	userRepo := identityPersistence.NewInMemoryUserRepository()
	sessionRepo := identityPersistence.NewInMemorySessionRepository()
	authUseCase := identityApp.NewAuthUseCase(userRepo, sessionRepo)
	// AuthMiddleware 由 identity 提供，保护全局路由；
	// 写入 ctx 的 UserID 由其他上下文的 handler 通过 middleware.UserIDFromContext 读取
	authMiddleware := identityMW.NewAuthMiddleware(authUseCase)

	// ── Catalog Context ──
	// productRepo 同时被 catalogUseCase 和 paymentCatalog ACL adapter 使用
	productRepo := catalogPersistence.NewInMemoryProductRepository()
	catalogUseCase := catalogApp.NewCatalogUseCase(productRepo)
	catalogHandler := catalogHTTP.NewCatalogHandler(catalogUseCase)

	// ── Card Context ──
	// cardRepo 在此处共享给 payment 侧的 ACL adapter（CardAdapter）
	// 这是唯一合法的跨上下文共享点；card/domain 和 payment/domain 互不可见
	cardRepo := cardPersistence.NewInMemoryCardRepository()
	fakeVault := cardVault.NewFakeCardVault()
	cardUseCase := cardApp.NewCardUseCase(cardRepo, fakeVault)
	cardHandler := cardHTTP.NewCardHandler(cardUseCase)

	// ── Payment Context ──
	// CatalogAdapter: ACL — 实现 payment 的 CatalogQuery 端口
	//   接收 catalog.ProductRepository，翻译为 payment 视角的 ProductView
	catalogAdapter := paymentCatalog.NewCatalogAdapter(productRepo)
	// CardAdapter: ACL — 实现 payment 的 CardQuery 端口
	//   接收 card.CardRepository（通过 cardPort.CardRepository 接口），
	//   翻译为 payment 视角的 SavedCardView（含 VaultToken）
	cardAdapter := paymentCard.NewCardAdapter(cardRepo)
	gateway := paymentGateway.NewMockPaymentGateway()
	txnRepo := paymentPersistence.NewInMemoryTransactionRepository()
	chargeUseCase := paymentApp.NewChargeUseCase(gateway, txnRepo, catalogAdapter, cardAdapter)
	paymentHandler := paymentHTTP.NewPaymentHandler(chargeUseCase)

	// ── 路由组装 ──
	mux := http.NewServeMux()
	catalogHandler.RegisterRoutes(mux) // GET /products, GET /products/{id}
	cardHandler.RegisterRoutes(mux)    // POST /cards, GET /cards, GET/DELETE /cards/{id}, 子路由见下
	paymentHandler.RegisterRoutes(mux) // POST /charge, POST /capture/{id}, POST /refund/{id}, GET /transaction/{id}

	// ── 中间件链 ──
	// Auth Middleware 包裹整个 mux，所有请求均需携带 Authorization: Bearer <token>
	app := authMiddleware.Handle(mux)

	addr := ":" + cfg.Port
	log.Printf("Payment Demo starting on %s (env=%s)", addr, cfg.Env)
	log.Println("")
	log.Println("  Identity:")
	log.Println("    认证: Authorization: Bearer token_alice")
	log.Println("")
	log.Println("  Catalog:")
	log.Println("    GET  /products              - 商品列表")
	log.Println("    GET  /products/{id}         - 商品详情")
	log.Println("")
	log.Println("  Card:")
	log.Println("    POST   /cards               - 绑卡（body: one_time_token）")
	log.Println("    GET    /cards               - 我的卡列表")
	log.Println("    GET    /cards/{id}          - 卡详情")
	log.Println("    DELETE /cards/{id}          - 删除卡（软删 + Vault 清除）")
	log.Println("    POST   /cards/{id}/suspend  - 挂起卡（Active → Suspended）")
	log.Println("    POST   /cards/{id}/activate - 激活卡（Suspended → Active）")
	log.Println("    POST   /cards/{id}/default  - 设为默认卡（仅 Active 可设）")
	log.Println("")
	log.Println("  Payment:")
	log.Println("    POST /charge                - 购买（支持 saved_card_id 使用已保存卡）")
	log.Println("    POST /capture/{id}          - 扣款")
	log.Println("    POST /refund/{id}           - 退款")
	log.Println("    GET  /transaction/{id}      - 查询交易")

	if err := http.ListenAndServe(addr, app); err != nil {
		log.Fatal(err)
	}
}
