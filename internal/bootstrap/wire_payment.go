package bootstrap

import (
	catalogPersistence "payment-demo/internal/catalog/adapter/persistence"
	cardPersistence "payment-demo/internal/card/adapter/persistence"
	couponInmem "payment-demo/internal/coupon/adapter/inmem"
	"payment-demo/internal/infra/paypal"
	"payment-demo/internal/infra/stripe"
	merchantPersistence "payment-demo/internal/merchant/adapter/persistence"

	paymentCard "payment-demo/internal/payment/adapter/card"
	paymentCatalog "payment-demo/internal/payment/adapter/catalog"
	paymentCoupon "payment-demo/internal/payment/adapter/coupon"
	paymentGateway "payment-demo/internal/payment/adapter/gateway"
	paymentMerchant "payment-demo/internal/payment/adapter/merchant"
	paymentPersistence "payment-demo/internal/payment/adapter/persistence"
	paymentTax "payment-demo/internal/payment/adapter/tax"
	paymentApp "payment-demo/internal/payment/application"
	paymentHTTP "payment-demo/internal/payment/handler/http"
)

// PaymentModule payment 上下文的组装产物。
// 依赖其他 4 个上下文的 Repository（通过 ACL adapter 隔离）。
type PaymentModule struct {
	Handler *paymentHTTP.PaymentHandler
}

// wirePayment 组装 payment 上下文。
// 接收各渠道共享的 infra 客户端和其他上下文共享的 Repository 指针。
func wirePayment(
	stripeClient *stripe.Client,
	paypalClient *paypal.Client,
	productRepo *catalogPersistence.InMemoryProductRepository,
	cardRepo *cardPersistence.InMemoryCardRepository,
	merchantRepo *merchantPersistence.InMemoryMerchantRepository,
	couponRepo *couponInmem.InMemoryCouponRepository,
) *PaymentModule {
	// ACL adapters — 实现 payment 侧消费方定义的 port 接口
	catalogAdapter := paymentCatalog.NewCatalogAdapter(productRepo)
	cardAdapter := paymentCard.NewCardAdapter(cardRepo)
	merchantAdapter := paymentMerchant.NewMerchantAdapter(merchantRepo)
	couponAdapter := paymentCoupon.NewCouponAdapter(couponRepo)

	// 多渠道网关工厂（Card → Stripe, PayPal → PayPal）+ 仓储
	gatewayFactory := paymentGateway.NewMultiChannelGatewayFactory(stripeClient, paypalClient)
	txnRepo := paymentPersistence.NewInMemoryTransactionRepository()

	// 税率查询（演示用静态配置：1000 BP = 10.00%）
	taxQuery := paymentTax.NewStaticTaxQuery(1000)

	uc := paymentApp.NewChargeUseCase(
		merchantAdapter, gatewayFactory, txnRepo,
		catalogAdapter, cardAdapter, couponAdapter, taxQuery,
	)
	return &PaymentModule{
		Handler: paymentHTTP.NewPaymentHandler(uc),
	}
}
