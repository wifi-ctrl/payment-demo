package bootstrap

import (
	catalogPersistence "payment-demo/internal/catalog/adapter/persistence"
	couponInmem "payment-demo/internal/coupon/adapter/inmem"
	orderCatalog "payment-demo/internal/order/adapter/catalog"
	orderCoupon "payment-demo/internal/order/adapter/coupon"
	orderPayment "payment-demo/internal/order/adapter/payment"
	orderPersistence "payment-demo/internal/order/adapter/persistence"
	orderTax "payment-demo/internal/order/adapter/tax"
	orderApp "payment-demo/internal/order/application"
	orderHTTP "payment-demo/internal/order/handler/http"
	acquiringApp "payment-demo/internal/acquiring/application"
)

type OrderModule struct {
	Handler *orderHTTP.OrderHandler
}

func wireOrder(
	productRepo *catalogPersistence.InMemoryProductRepository,
	couponRepo *couponInmem.InMemoryCouponRepository,
	chargeUC *acquiringApp.ChargeUseCase,
) *OrderModule {
	catalogAdapter := orderCatalog.NewCatalogAdapter(productRepo)
	couponAdapter := orderCoupon.NewCouponAdapter(couponRepo)
	taxQuery := orderTax.NewStaticTaxQuery(1000)
	paymentAdapter := orderPayment.NewPaymentCommandAdapter(chargeUC)
	orderRepo := orderPersistence.NewInMemoryOrderRepository()

	uc := orderApp.NewOrderUseCase(orderRepo, catalogAdapter, couponAdapter, taxQuery, paymentAdapter)
	return &OrderModule{
		Handler: orderHTTP.NewOrderHandler(uc),
	}
}
