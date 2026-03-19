package main

import (
	"log"
	"net/http"

	"payment-demo/internal/bootstrap"
	"payment-demo/internal/infra/config"
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
	log.Println("    POST   /cards               - 绑卡")
	log.Println("    POST   /cards/tokenize      - 卡令牌化（PAN → ct_token）")
	log.Println("    GET    /cards               - 我的卡列表")
	log.Println("    GET    /cards?id=xxx        - 卡详情")
	log.Println("    DELETE /cards               - 删除卡")
	log.Println("")
	log.Println("  Merchant:")
	log.Println("    POST   /merchants                    - 注册商户")
	log.Println("    GET    /merchants                    - 商户列表")
	log.Println("    POST   /merchants/credentials        - 添加渠道凭据")
	log.Println("    POST   /merchants/suspend            - 暂停商户")
	log.Println("")
	log.Println("  Coupon:")
	log.Println("    POST /coupons               - 创建优惠券")
	log.Println("    GET  /coupons?code=SAVE10   - 按编码查询优惠券")
	log.Println("")
	log.Println("  Payment:")
	log.Println("    POST /charge                - Card 购买")
	log.Println("    POST /charge/paypal         - PayPal 购买")
	log.Println("    POST /capture               - 扣款")
	log.Println("    POST /refund                - 退款")
	log.Println("    GET  /transaction?id=xxx    - 查询交易")
}
