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
	cardPersistence "payment-demo/internal/card/adapter/persistence"
	cardVault "payment-demo/internal/card/adapter/vault"
	cardApp "payment-demo/internal/card/application"
	cardHTTP "payment-demo/internal/card/handler/http"

	// ── Merchant Context ──
	// merchantRepo 同时被 merchant usecase 和 payment 侧 ACL adapter 持有
	// 跨上下文共享 Repository 指针只允许在 main.go（Composition Root）进行
	merchantPersistence "payment-demo/internal/merchant/adapter/persistence"
	merchantApp "payment-demo/internal/merchant/application"
	merchantHTTP "payment-demo/internal/merchant/handler/http"

	// ── Payment Context ──
	// paymentCard / paymentCatalog / paymentMerchant 均为 ACL adapter，
	// 实现 payment 侧消费方定义的 port 接口，隔离跨上下文依赖
	paymentCard "payment-demo/internal/payment/adapter/card"
	paymentCatalog "payment-demo/internal/payment/adapter/catalog"
	paymentGateway "payment-demo/internal/payment/adapter/gateway"
	paymentMerchant "payment-demo/internal/payment/adapter/merchant"
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
	userRepo := identityPersistence.NewInMemoryUserRepository()
	sessionRepo := identityPersistence.NewInMemorySessionRepository()
	authUseCase := identityApp.NewAuthUseCase(userRepo, sessionRepo)
	// AuthMiddleware 由 identity 提供，写入 ctx 的 UserID 由其他上下文 handler 读取
	authMiddleware := identityMW.NewAuthMiddleware(authUseCase)

	// ── Catalog Context ──
	productRepo := catalogPersistence.NewInMemoryProductRepository()
	catalogUseCase := catalogApp.NewCatalogUseCase(productRepo)
	catalogHandler := catalogHTTP.NewCatalogHandler(catalogUseCase)

	// ── Card Context ──
	// cardRepo 在此处共享给 payment 侧的 ACL adapter（CardAdapter）
	cardRepo := cardPersistence.NewInMemoryCardRepository()
	fakeVault := cardVault.NewFakeCardVault()
	cardUseCase := cardApp.NewCardUseCase(cardRepo, fakeVault)
	cardHandler := cardHTTP.NewCardHandler(cardUseCase)

	// ── Merchant Context ──
	// merchantRepo 共享给 payment 侧的 MerchantAdapter（ACL），合法的跨上下文依赖注入点
	merchantRepo := merchantPersistence.NewInMemoryMerchantRepository()
	merchantUseCase := merchantApp.NewMerchantUseCase(merchantRepo)
	merchantHandler := merchantHTTP.NewMerchantHandler(merchantUseCase)

	// ── Payment Context ──
	// CatalogAdapter: ACL — 实现 payment 的 CatalogQuery 端口
	catalogAdapter := paymentCatalog.NewCatalogAdapter(productRepo)
	// CardAdapter: ACL — 实现 payment 的 CardQuery 端口
	cardAdapter := paymentCard.NewCardAdapter(cardRepo)
	// MerchantAdapter: ACL — 实现 payment 的 MerchantQuery 端口（多商户路由核心）
	// 跨上下文 import 仅在此 adapter 包，符合架构约束
	merchantAdapter := paymentMerchant.NewMerchantAdapter(merchantRepo)
	// GatewayFactory: 根据商户凭据动态构建 Card/PayPal Gateway 实例（多商户隔离）
	gatewayFactory := paymentGateway.NewMockGatewayFactory()
	txnRepo := paymentPersistence.NewInMemoryTransactionRepository()

	// NewChargeUseCase 注入 MerchantQuery（多商户路由）+ GatewayFactory（动态 Gateway 构建）
	// 替代原有静态注入 gateway + paypalGateway，实现商户级别的支付渠道隔离
	chargeUseCase := paymentApp.NewChargeUseCase(merchantAdapter, gatewayFactory, txnRepo, catalogAdapter, cardAdapter)
	paymentHandler := paymentHTTP.NewPaymentHandler(chargeUseCase)

	// ── 路由组装 ──
	mux := http.NewServeMux()
	catalogHandler.RegisterRoutes(mux)  // GET /products, GET /products/{id}
	cardHandler.RegisterRoutes(mux)     // POST /cards, GET /cards, GET/DELETE /cards/{id}, 子路由
	merchantHandler.RegisterRoutes(mux) // POST/GET /merchants, 子路由 /credentials /suspend
	paymentHandler.RegisterRoutes(mux)  // POST /charge, POST /charge/paypal, POST /capture/{id}, POST /refund/{id}, GET /transaction/{id}

	// ── 中间件链 ──
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
	log.Println("    POST   /cards               - 绑卡")
	log.Println("    GET    /cards               - 我的卡列表")
	log.Println("    GET    /cards/{id}          - 卡详情")
	log.Println("    DELETE /cards/{id}          - 删除卡")
	log.Println("    POST   /cards/{id}/suspend  - 挂起卡")
	log.Println("    POST   /cards/{id}/activate - 激活卡")
	log.Println("    POST   /cards/{id}/default  - 设为默认卡")
	log.Println("")
	log.Println("  Merchant（多商户管理）:")
	log.Println("    POST   /merchants                                    - 注册商户")
	log.Println("    GET    /merchants                                    - 商户列表")
	log.Println("    GET    /merchants/{id}                               - 商户详情")
	log.Println("    POST   /merchants/{id}/credentials                   - 添加渠道凭据")
	log.Println("    DELETE /merchants/{id}/credentials/{credID}          - 吊销凭据")
	log.Println("    POST   /merchants/{id}/suspend                       - 暂停商户")
	log.Println("")
	log.Println("  Payment（支持多商户，请求体须携带 merchant_id）:")
	log.Println("    POST /charge                - Card 购买（merchant_id + token_id/saved_card_id）")
	log.Println("    POST /charge/paypal         - PayPal 购买（merchant_id + order_id + payer_id）")
	log.Println("    POST /capture/{id}          - 扣款（按 MerchantID+Method 路由 Gateway）")
	log.Println("    POST /refund/{id}           - 退款（按 MerchantID+Method 路由 Gateway）")
	log.Println("    GET  /transaction/{id}      - 查询交易")

	if err := http.ListenAndServe(addr, app); err != nil {
		log.Fatal(err)
	}
}
