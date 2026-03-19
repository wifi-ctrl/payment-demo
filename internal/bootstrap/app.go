// Package bootstrap 是 Composition Root 的实现包。
//
// 职责：组装所有限界上下文的依赖，将 adapter 注入 UseCase，将 UseCase 注入 handler。
// 这是系统中唯一知道所有上下文存在的地方（与 main.go 共同构成 Composition Root）。
//
// 每个上下文的组装逻辑封装在独立的 wire_<context>.go 文件中。
package bootstrap

import (
	"net/http"

	"payment-demo/internal/infra/config"
	"payment-demo/internal/infra/paypal"
	"payment-demo/internal/infra/stripe"
)

// App 应用实例，持有组装完毕的 HTTP handler。
type App struct {
	handler http.Handler
}

// New 组装所有上下文并返回可运行的 App。
func New(cfg *config.Config) *App {
	// 0. 共享基础设施：各渠道 HTTP 客户端（Demo 使用内嵌 mock server）
	stripeClient := stripe.NewMockClient(cfg.StripeAPIKey) // Card 渠道（card + payment 共用）
	paypalClient := paypal.NewMockClient()                 // PayPal 渠道（payment 使用）

	// 1. 各上下文独立组装（无跨上下文依赖的先组装）
	identity := wireIdentity()
	catalog := wireCatalog()
	card := wireCard(stripeClient)
	merchant := wireMerchant()
	coupon := wireCoupon()

	// 2. payment 依赖其他上下文的 Repository（通过 ACL adapter 隔离）
	payment := wirePayment(
		stripeClient,
		paypalClient,
		catalog.ProductRepo,
		card.CardRepo,
		merchant.MerchantRepo,
		coupon.CouponRepo,
	)

	// 3. 路由注册
	mux := http.NewServeMux()
	catalog.Handler.RegisterRoutes(mux)
	card.Handler.RegisterRoutes(mux)
	merchant.Handler.RegisterRoutes(mux)
	coupon.Handler.RegisterRoutes(mux)
	payment.Handler.RegisterRoutes(mux)

	// 4. 中间件链
	return &App{handler: identity.Middleware.Handle(mux)}
}

// Handler 返回组装完毕的 http.Handler，供 main.go 启动 HTTP Server。
func (a *App) Handler() http.Handler {
	return a.handler
}
