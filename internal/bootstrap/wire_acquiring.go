package bootstrap

import (
	cardPersistence "payment-demo/internal/card/adapter/persistence"
	cardApp "payment-demo/internal/card/application"
	"payment-demo/internal/config"

	acquiringCard "payment-demo/internal/acquiring/adapter/card"
	acquiringGateway "payment-demo/internal/acquiring/adapter/gateway"
	gwPaypal "payment-demo/internal/acquiring/adapter/gateway/paypal"
	gwStripe "payment-demo/internal/acquiring/adapter/gateway/stripe"
	acquiringPersistence "payment-demo/internal/acquiring/adapter/persistence"
	acquiringApp "payment-demo/internal/acquiring/application"
	acquiringHTTP "payment-demo/internal/acquiring/handler/http"
)

// AcquiringModule acquiring 上下文的组装产物（合并了原 merchant + payment）。
type AcquiringModule struct {
	PaymentHandler  *acquiringHTTP.PaymentHandler
	MerchantHandler *acquiringHTTP.MerchantHandler
	ChargeUC        *acquiringApp.ChargeUseCase
	MerchantRepo    *acquiringPersistence.InMemoryMerchantRepository
}

// wireAcquiring 组装 acquiring 上下文（merchant + payment）。
func wireAcquiring(
	cfg *config.Config,
	cardRepo *cardPersistence.InMemoryCardRepository,
	cardUC *cardApp.CardUseCase,
) *AcquiringModule {
	// Merchant（acquiring 内部）
	merchantRepo := acquiringPersistence.NewInMemoryMerchantRepository()
	merchantUC := acquiringApp.NewMerchantUseCase(merchantRepo)
	merchantHandler := acquiringHTTP.NewMerchantHandler(merchantUC)

	// Gateway clients（acquiring 内部关注）
	stripeClient := gwStripe.NewMockClient(cfg.StripeAPIKey)
	paypalClient := gwPaypal.NewMockClient()

	// Card ACL adapters
	cardQueryAdapter := acquiringCard.NewCardAdapter(cardRepo)
	cardCommandAdapter := acquiringCard.NewCardCommandAdapter(cardUC)

	// Gateway factory + Transaction repo
	gatewayFactory := acquiringGateway.NewMultiChannelGatewayFactory(stripeClient, paypalClient)
	txnRepo := acquiringPersistence.NewInMemoryTransactionRepository()

	chargeUC := acquiringApp.NewChargeUseCase(
		merchantRepo, gatewayFactory, txnRepo,
		cardQueryAdapter, cardCommandAdapter,
	)

	paymentHandler := acquiringHTTP.NewPaymentHandler(chargeUC, cfg.RecurringWebhookSecret)

	return &AcquiringModule{
		PaymentHandler:  paymentHandler,
		MerchantHandler: merchantHandler,
		ChargeUC:        chargeUC,
		MerchantRepo:    merchantRepo,
	}
}
