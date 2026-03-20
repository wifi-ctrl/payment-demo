// Package bootstrap 是 Composition Root 的实现包。
//
// 职责：组装所有限界上下文的依赖，将 adapter 注入 UseCase，将 UseCase 注入 handler。
// 这是系统中唯一知道所有上下文存在的地方（与 main.go 共同构成 Composition Root）。
//
// 每个上下文的组装逻辑封装在独立的 wire_<context>.go 文件中。
package bootstrap

import (
	"net/http"

	"payment-demo/internal/config"
)

// App 应用实例，持有组装完毕的 HTTP handler。
type App struct {
	handler http.Handler
}

// New 组装所有上下文并返回可运行的 App。
func New(cfg *config.Config) *App {
	// 1. 独立上下文
	identity := wireIdentity()
	catalog := wireCatalog()
	card := wireCard()
	coupon := wireCoupon()

	// 2. Acquiring 合并了 merchant + payment，依赖 card
	acquiring := wireAcquiring(cfg, card.CardRepo, card.CardUC)

	// 3. Order 依赖 catalog + coupon + Acquiring.ChargeUseCase
	order := wireOrder(
		catalog.ProductRepo,
		coupon.CouponRepo,
		acquiring.ChargeUC,
	)

	// 4. 路由注册
	mux := http.NewServeMux()
	catalog.Handler.RegisterRoutes(mux)
	card.Handler.RegisterRoutes(mux)
	acquiring.MerchantHandler.RegisterRoutes(mux)
	coupon.Handler.RegisterRoutes(mux)
	acquiring.PaymentHandler.RegisterRoutes(mux)
	order.Handler.RegisterRoutes(mux)

	return &App{handler: identity.Middleware.Handle(mux)}
}

// Handler 返回组装完毕的 http.Handler，供 main.go 启动 HTTP Server。
func (a *App) Handler() http.Handler {
	return a.handler
}
