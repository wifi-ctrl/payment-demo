package main

import (
	"log"
	"net/http"

	"payment-demo/internal/bootstrap"
	"payment-demo/internal/config"
)

func main() {
	cfg := config.Load()
	app := bootstrap.New(cfg)

	addr := ":" + cfg.Port
	log.Printf("Payment Demo starting on %s (env=%s)", addr, cfg.Env)
	printRoutes()

	if err := http.ListenAndServe(addr, app.Handler()); err != nil {
		log.Fatal(err)
	}
}

func printRoutes() {
	log.Println("")
	log.Println("  Identity:")
	log.Println("    认证: Authorization: Bearer token_alice")
	log.Println("")
	log.Println("  Catalog:")
	log.Println("    GET  /products              - 商品列表")
	log.Println("    GET  /products?id=xxx       - 商品详情")
	log.Println("")
	log.Println("  Card:")
	log.Println("    POST   /cards/tokenize      - 卡令牌化（PAN → 临时 ct_token；绑卡仅在支付 Capture 成功后由系统触发）")
	log.Println("    GET    /cards               - 我的卡列表")
	log.Println("    GET    /cards?id=xxx        - 卡详情")
	log.Println("    DELETE /cards               - 删除卡")
	log.Println("    POST   /cards/suspend        - 暂停卡")
	log.Println("    POST   /cards/activate      - 激活卡")
	log.Println("    POST   /cards/default       - 设为默认卡（亦支持 PUT）")
	log.Println("    (POST /cards 已禁用 — 禁止未经验证的直接绑卡)")
	log.Println("")
	log.Println("  Acquiring (Merchant):")
	log.Println("    POST   /merchants                    - 注册商户")
	log.Println("    GET    /merchants                    - 商户列表")
	log.Println("    POST   /merchants/credentials        - 添加渠道凭据")
	log.Println("    POST   /merchants/suspend            - 暂停商户")
	log.Println("")
	log.Println("  Coupon:")
	log.Println("    POST /coupons               - 创建优惠券")
	log.Println("    GET  /coupons?code=SAVE10   - 按编码查询优惠券")
	log.Println("")
	log.Println("  Order:")
	log.Println("    POST /orders                - 创建订单（查商品+定价+发起支付授权）")
	log.Println("    GET  /orders?id=xxx         - 查询订单")
	log.Println("    POST /orders/capture        - 扣款 {\"order_id\":\"xxx\"}")
	log.Println("    POST /orders/refund         - 退款 {\"order_id\":\"xxx\"}")
	log.Println("")
	log.Println("  Acquiring (Payment — internal):")
	log.Println("    GET  /internal/transaction?id=xxx - 查询交易（内部端点）")
	log.Println("    POST /webhooks/recurring-token    - 异步 recurring token（若设置 RECURRING_WEBHOOK_SECRET 则需 X-Webhook-Secret 头）")
}
