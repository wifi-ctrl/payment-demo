package gateway_test

import (
	"context"
	"testing"

	"payment-demo/internal/payment/adapter/gateway"
	"payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// ─────────────────────────────────────────────────────────────────
// AC-49: GatewayFactory 按凭据 Secrets 动态构造不同商户的 Gateway 实例
// ─────────────────────────────────────────────────────────────────

func TestMockGatewayFactory_BuildCardGateway_ValidSecrets_ReturnsGateway(t *testing.T) {
	// AC-49: 有效凭据构造成功
	factory := gateway.NewMockGatewayFactory()
	cred := port.ChannelCredentialView{
		CredentialID: "cred-A",
		MerchantID:   "merchant-A",
		Channel:      "CARD",
		Secrets:      map[string]string{"api_key": "sk_A"},
	}

	gw, err := factory.BuildCardGateway(cred)

	if err != nil {
		t.Fatalf("BuildCardGateway: want nil error, got %v", err)
	}
	if gw == nil {
		t.Fatal("BuildCardGateway: want non-nil gateway")
	}
}

func TestMockGatewayFactory_BuildCardGateway_MissingAPIKey_ReturnsError(t *testing.T) {
	// AC-49: 缺少 api_key 时返回错误
	factory := gateway.NewMockGatewayFactory()
	cred := port.ChannelCredentialView{
		CredentialID: "cred-B",
		MerchantID:   "merchant-B",
		Channel:      "CARD",
		Secrets:      map[string]string{}, // 无 api_key
	}

	gw, err := factory.BuildCardGateway(cred)

	if err == nil {
		t.Error("BuildCardGateway with missing api_key: want error, got nil")
	}
	if gw != nil {
		t.Error("BuildCardGateway: want nil gateway on error")
	}
}

func TestMockGatewayFactory_BuildPayPalGateway_ValidSecrets_ReturnsGateway(t *testing.T) {
	// AC-49: PayPal 凭据构造成功
	factory := gateway.NewMockGatewayFactory()
	cred := port.ChannelCredentialView{
		CredentialID: "cred-paypal",
		MerchantID:   "merchant-pp",
		Channel:      "PAYPAL",
		Secrets: map[string]string{
			"client_id":     "paypal-client-id",
			"client_secret": "paypal-secret",
		},
	}

	gw, err := factory.BuildPayPalGateway(cred)

	if err != nil {
		t.Fatalf("BuildPayPalGateway: want nil error, got %v", err)
	}
	if gw == nil {
		t.Fatal("BuildPayPalGateway: want non-nil gateway")
	}
}

func TestMockGatewayFactory_BuildPayPalGateway_MissingClientID_ReturnsError(t *testing.T) {
	factory := gateway.NewMockGatewayFactory()
	cred := port.ChannelCredentialView{
		CredentialID: "cred-paypal",
		MerchantID:   "merchant-pp",
		Channel:      "PAYPAL",
		Secrets:      map[string]string{"client_secret": "secret"}, // 缺 client_id
	}

	gw, err := factory.BuildPayPalGateway(cred)

	if err == nil {
		t.Error("want error for missing client_id, got nil")
	}
	if gw != nil {
		t.Error("want nil gateway on error")
	}
}

func TestMockGatewayFactory_BuildPayPalGateway_MissingClientSecret_ReturnsError(t *testing.T) {
	factory := gateway.NewMockGatewayFactory()
	cred := port.ChannelCredentialView{
		CredentialID: "cred-paypal",
		MerchantID:   "merchant-pp",
		Channel:      "PAYPAL",
		Secrets:      map[string]string{"client_id": "id"}, // 缺 client_secret
	}

	gw, err := factory.BuildPayPalGateway(cred)

	if err == nil {
		t.Error("want error for missing client_secret, got nil")
	}
	if gw != nil {
		t.Error("want nil gateway on error")
	}
}

func TestMockGatewayFactory_BuildCardGateway_TwoDifferentMerchants_ReturnDistinctInstances(t *testing.T) {
	// AC-49: 两商户构造出不同 Gateway 实例
	factory := gateway.NewMockGatewayFactory()
	credA := port.ChannelCredentialView{
		CredentialID: "cred-A",
		MerchantID:   "merchant-A",
		Channel:      "CARD",
		Secrets:      map[string]string{"api_key": "sk_A"},
	}
	credB := port.ChannelCredentialView{
		CredentialID: "cred-B",
		MerchantID:   "merchant-B",
		Channel:      "CARD",
		Secrets:      map[string]string{"api_key": "sk_B"},
	}

	gwA, errA := factory.BuildCardGateway(credA)
	gwB, errB := factory.BuildCardGateway(credB)

	if errA != nil || errB != nil {
		t.Fatalf("BuildCardGateway errors: A=%v, B=%v", errA, errB)
	}
	// 实例不同（不是同一个指针）
	if gwA == gwB {
		t.Error("BuildCardGateway for different merchants must return different instances")
	}
}

func TestMockGatewayFactory_BuildPayPalGateway_TwoDifferentMerchants_ReturnDistinctInstances(t *testing.T) {
	// AC-49: PayPal 两商户构造出不同实例
	factory := gateway.NewMockGatewayFactory()
	credA := port.ChannelCredentialView{
		CredentialID: "cred-pp-A",
		MerchantID:   "merchant-A",
		Channel:      "PAYPAL",
		Secrets:      map[string]string{"client_id": "id_A", "client_secret": "sec_A"},
	}
	credB := port.ChannelCredentialView{
		CredentialID: "cred-pp-B",
		MerchantID:   "merchant-B",
		Channel:      "PAYPAL",
		Secrets:      map[string]string{"client_id": "id_B", "client_secret": "sec_B"},
	}

	gwA, errA := factory.BuildPayPalGateway(credA)
	gwB, errB := factory.BuildPayPalGateway(credB)

	if errA != nil || errB != nil {
		t.Fatalf("BuildPayPalGateway errors: A=%v, B=%v", errA, errB)
	}
	if gwA == gwB {
		t.Error("BuildPayPalGateway for different merchants must return different instances")
	}
}

// ─────────────────────────────────────────────────────────────────
// GatewayFactory 实现接口检查（编译期保障补充运行期验证）
// ─────────────────────────────────────────────────────────────────

func TestMockGatewayFactory_ImplementsInterface(t *testing.T) {
	var _ port.GatewayFactory = gateway.NewMockGatewayFactory()
}

// ─────────────────────────────────────────────────────────────────
// 验证构造出的 Gateway 可正常调用 Authorize
// ─────────────────────────────────────────────────────────────────

func TestMockGatewayFactory_BuildCardGateway_GatewayCanAuthorize(t *testing.T) {
	factory := gateway.NewMockGatewayFactory()
	cred := port.ChannelCredentialView{
		CredentialID: "cred-A",
		MerchantID:   "merchant-A",
		Channel:      "CARD",
		Secrets:      map[string]string{"api_key": "sk_A"},
	}

	gw, err := factory.BuildCardGateway(cred)
	if err != nil {
		t.Fatalf("BuildCardGateway: %v", err)
	}

	token := model.CardToken{TokenID: "tok_visa", Last4: "4242", Brand: "Visa"}
	amount := model.NewMoney(1000, "USD")
	result, authErr := gw.Authorize(context.Background(), token, amount)

	if authErr != nil {
		t.Fatalf("Authorize: %v", authErr)
	}
	if result == nil {
		t.Fatal("Authorize: want non-nil result")
	}
	if result.ProviderRef == "" {
		t.Error("Authorize: ProviderRef must not be empty")
	}
}

func TestMockGatewayFactory_BuildPayPalGateway_GatewayCanAuthorize(t *testing.T) {
	factory := gateway.NewMockGatewayFactory()
	cred := port.ChannelCredentialView{
		CredentialID: "cred-pp",
		MerchantID:   "merchant-pp",
		Channel:      "PAYPAL",
		Secrets:      map[string]string{"client_id": "id", "client_secret": "sec"},
	}

	gw, err := factory.BuildPayPalGateway(cred)
	if err != nil {
		t.Fatalf("BuildPayPalGateway: %v", err)
	}

	token := model.PayPalToken{OrderID: "5O190127TN364715T", PayerID: "PAYER-1"}
	amount := model.NewMoney(2000, "USD")
	result, authErr := gw.Authorize(context.Background(), token, amount)

	if authErr != nil {
		t.Fatalf("PayPal Authorize: %v", authErr)
	}
	if result == nil {
		t.Fatal("PayPal Authorize: want non-nil result")
	}
}
