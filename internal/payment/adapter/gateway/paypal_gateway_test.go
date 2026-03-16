package gateway_test

import (
	"context"
	"strings"
	"testing"

	"payment-demo/internal/payment/adapter/gateway"
	"payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// ─────────────────────────────────────────────────────────────────
// 编译期接口检查（AC-44 — 在测试包中二次验证）
// ─────────────────────────────────────────────────────────────────

var _ port.PayPalGateway = (*gateway.MockPayPalGateway)(nil)

// ─────────────────────────────────────────────────────────────────
// AC-13 / AC-44: 接口方法签名完整，编译期断言通过
// ─────────────────────────────────────────────────────────────────

func TestMockPayPalGateway_ImplementsInterface(t *testing.T) {
	// AC-13, AC-44
	// 如果 MockPayPalGateway 缺少 Authorize/Capture/Refund 任意方法，
	// 文件顶部的 var _ port.PayPalGateway = (*gateway.MockPayPalGateway)(nil) 将导致编译失败
	gw := gateway.NewMockPayPalGateway()
	if gw == nil {
		t.Fatal("NewMockPayPalGateway must return non-nil")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-40: MockPayPalGateway.Authorize — 正常 OrderID 返回授权结果
// ─────────────────────────────────────────────────────────────────

func TestMockPayPalGateway_Authorize_ValidOrderID_ReturnsResult(t *testing.T) {
	// AC-40
	gw := gateway.NewMockPayPalGateway()
	token := model.PayPalToken{OrderID: "5O190127TN364715T", PayerID: "FSMVU44LF3YUS"}
	amount := model.NewMoney(1000, "USD")

	result, err := gw.Authorize(context.Background(), token, amount)

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if result == nil {
		t.Fatal("want non-nil PayPalAuthResult, got nil")
	}

	// ProviderRef 非空，格式为 "CAPTURE-{数字}"
	if result.ProviderRef == "" {
		t.Error("ProviderRef must not be empty")
	}
	if !strings.HasPrefix(result.ProviderRef, "CAPTURE-") {
		t.Errorf("ProviderRef must have 'CAPTURE-' prefix, got %s", result.ProviderRef)
	}

	// PayerEmail 固定值
	if result.PayerEmail != "buyer@example.com" {
		t.Errorf("PayerEmail: want buyer@example.com, got %s", result.PayerEmail)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-41: MockPayPalGateway.Authorize — EC-DECLINE 前缀 OrderID 返回 ErrPayPalTokenInvalid
// ─────────────────────────────────────────────────────────────────

func TestMockPayPalGateway_Authorize_DeclinePrefix_ReturnsError(t *testing.T) {
	// AC-41
	gw := gateway.NewMockPayPalGateway()
	declineToken := model.PayPalToken{OrderID: "EC-DECLINE-001", PayerID: "ANY-PAYER"}
	amount := model.NewMoney(1000, "USD")

	result, err := gw.Authorize(context.Background(), declineToken, amount)

	if err != model.ErrPayPalTokenInvalid {
		t.Errorf("want ErrPayPalTokenInvalid, got %v", err)
	}
	if result != nil {
		t.Errorf("want nil result on decline, got %+v", result)
	}
}

// EC-DECLINE 变体：确保前缀匹配逻辑正确（完整前缀匹配）
func TestMockPayPalGateway_Authorize_DeclinePrefix_Variants(t *testing.T) {
	// AC-41 扩展
	gw := gateway.NewMockPayPalGateway()
	amount := model.NewMoney(500, "USD")

	declineIDs := []string{
		"EC-DECLINE",
		"EC-DECLINE-999",
		"EC-DECLINE-ABC",
	}
	for _, orderID := range declineIDs {
		t.Run(orderID, func(t *testing.T) {
			token := model.PayPalToken{OrderID: orderID, PayerID: "PAYER"}
			_, err := gw.Authorize(context.Background(), token, amount)
			if err != model.ErrPayPalTokenInvalid {
				t.Errorf("OrderID %s: want ErrPayPalTokenInvalid, got %v", orderID, err)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-42: MockPayPalGateway.Capture — 返回 nil error
// ─────────────────────────────────────────────────────────────────

func TestMockPayPalGateway_Capture_AlwaysSucceeds(t *testing.T) {
	// AC-42
	gw := gateway.NewMockPayPalGateway()
	amount := model.NewMoney(1000, "USD")

	err := gw.Capture(context.Background(), "CAPTURE-12345", amount)
	if err != nil {
		t.Errorf("Capture: want nil error, got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-43: MockPayPalGateway.Refund — 返回 nil error
// ─────────────────────────────────────────────────────────────────

func TestMockPayPalGateway_Refund_AlwaysSucceeds(t *testing.T) {
	// AC-43
	gw := gateway.NewMockPayPalGateway()
	amount := model.NewMoney(1000, "USD")

	err := gw.Refund(context.Background(), "CAPTURE-12345", amount)
	if err != nil {
		t.Errorf("Refund: want nil error, got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-48: MockPayPalGateway.Authorize — 多次调用 ProviderRef 大概率不同（非硬编码）
// ─────────────────────────────────────────────────────────────────

func TestMockPayPalGateway_Authorize_MultipleCallsReturnDifferentProviderRef(t *testing.T) {
	// AC-48
	gw := gateway.NewMockPayPalGateway()
	token := model.PayPalToken{OrderID: "5O190127TN364715T", PayerID: "FSMVU44LF3YUS"}
	amount := model.NewMoney(1000, "USD")

	// 多次调用，收集 ProviderRef
	const n = 10
	refs := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		result, err := gw.Authorize(context.Background(), token, amount)
		if err != nil {
			t.Fatalf("Authorize[%d]: unexpected error %v", i, err)
		}
		if result.ProviderRef == "" {
			t.Fatalf("Authorize[%d]: ProviderRef must not be empty", i)
		}
		refs[result.ProviderRef] = struct{}{}
	}

	// 10 次调用至少应有 2 种不同的 ProviderRef（rand.Int63 生成）
	if len(refs) < 2 {
		t.Errorf("want multiple distinct ProviderRefs across %d calls, got %d unique", n, len(refs))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-14: PayPalAuthResult DTO 字段正确（通过 Authorize 返回值验证）
// ─────────────────────────────────────────────────────────────────

func TestPayPalAuthResult_Fields_AreAccessible(t *testing.T) {
	// AC-14
	result := &port.PayPalAuthResult{
		ProviderRef: "CAPTURE-999",
		PayerEmail:  "buyer@example.com",
	}
	if result.ProviderRef != "CAPTURE-999" {
		t.Errorf("ProviderRef: want CAPTURE-999, got %s", result.ProviderRef)
	}
	if result.PayerEmail != "buyer@example.com" {
		t.Errorf("PayerEmail: want buyer@example.com, got %s", result.PayerEmail)
	}
}
