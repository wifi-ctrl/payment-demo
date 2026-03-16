package model_test

import (
	"testing"
	"time"

	"payment-demo/internal/payment/domain/event"
	"payment-demo/internal/payment/domain/model"
)

// ─────────────────────────────────────────────────────────────────
// 辅助构造函数
// ─────────────────────────────────────────────────────────────────

func validPayPalToken() model.PayPalToken {
	return model.PayPalToken{OrderID: "5O190127TN364715T", PayerID: "FSMVU44LF3YUS"}
}

func validMoney() model.Money {
	return model.NewMoney(1000, "USD")
}

func newPayPalTxn() *model.PaymentTransaction {
	return model.NewPayPalTransaction("user-1", "prod-1", validMoney(), validPayPalToken())
}

// newPayPalTxnAuthorized 返回已通过 MarkAuthorized 的 PayPal 交易（Events 未清空）
func newPayPalTxnAuthorized() *model.PaymentTransaction {
	txn := newPayPalTxn()
	_ = txn.MarkAuthorized("CAPTURE-123", "")
	return txn
}

// newPayPalTxnCaptured 返回已通过 MarkCaptured 的 PayPal 交易（Events 未清空）
func newPayPalTxnCaptured() *model.PaymentTransaction {
	txn := newPayPalTxnAuthorized()
	txn.ClearEvents() // 清空 Authorized 事件，方便后续 Captured 事件计数
	_ = txn.MarkCaptured()
	return txn
}

// ─────────────────────────────────────────────────────────────────
// AC-1: NewPayPalTransaction 工厂方法 — 正常创建
// ─────────────────────────────────────────────────────────────────

func TestNewPayPalTransaction_Factory_CreatesWithCorrectFields(t *testing.T) {
	// AC-1
	before := time.Now()
	token := validPayPalToken()
	amount := validMoney()

	txn := model.NewPayPalTransaction("user-42", "prod-99", amount, token)
	after := time.Now()

	// ID 非空（uuid 格式，长度为 36）
	if txn.ID == "" {
		t.Error("ID must not be empty")
	}
	if len(string(txn.ID)) != 36 {
		t.Errorf("ID length want 36 (uuid), got %d", len(string(txn.ID)))
	}

	// Status == StatusCreated
	if txn.Status != model.StatusCreated {
		t.Errorf("Status: want CREATED, got %s", txn.Status)
	}

	// Method == PaymentMethodPayPal
	if txn.Method != model.PaymentMethodPayPal {
		t.Errorf("Method: want PAYPAL, got %s", txn.Method)
	}

	// PayPalToken 字段正确
	if txn.PayPalToken.OrderID != "5O190127TN364715T" {
		t.Errorf("PayPalToken.OrderID: want 5O190127TN364715T, got %s", txn.PayPalToken.OrderID)
	}
	if txn.PayPalToken.PayerID != "FSMVU44LF3YUS" {
		t.Errorf("PayPalToken.PayerID: want FSMVU44LF3YUS, got %s", txn.PayPalToken.PayerID)
	}

	// CardToken 为零值
	if txn.CardToken.TokenID != "" || txn.CardToken.Last4 != "" || txn.CardToken.Brand != "" {
		t.Errorf("CardToken must be zero value, got %+v", txn.CardToken)
	}

	// Amount 正确
	if txn.Amount.Amount != 1000 || txn.Amount.Currency != "USD" {
		t.Errorf("Amount: want {1000 USD}, got %+v", txn.Amount)
	}

	// Events 为空
	if len(txn.Events) != 0 {
		t.Errorf("Events must be empty on creation, got %d", len(txn.Events))
	}

	// CreatedAt 不为零值，且在调用区间内
	if txn.CreatedAt.IsZero() {
		t.Error("CreatedAt must not be zero")
	}
	if txn.CreatedAt.Before(before) || txn.CreatedAt.After(after) {
		t.Errorf("CreatedAt out of expected range: %v", txn.CreatedAt)
	}

	// UserID / ProductID 正确透传
	if txn.UserID != "user-42" {
		t.Errorf("UserID: want user-42, got %s", txn.UserID)
	}
	if txn.ProductID != "prod-99" {
		t.Errorf("ProductID: want prod-99, got %s", txn.ProductID)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-2: PaymentMethod 枚举值正确
// ─────────────────────────────────────────────────────────────────

func TestPaymentMethod_EnumValues_AreCorrect(t *testing.T) {
	// AC-2
	if string(model.PaymentMethodCard) != "CARD" {
		t.Errorf("PaymentMethodCard: want CARD, got %s", model.PaymentMethodCard)
	}
	if string(model.PaymentMethodPayPal) != "PAYPAL" {
		t.Errorf("PaymentMethodPayPal: want PAYPAL, got %s", model.PaymentMethodPayPal)
	}
	if model.PaymentMethodCard == model.PaymentMethodPayPal {
		t.Error("PaymentMethodCard and PaymentMethodPayPal must not be equal")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-3: PayPalToken 零值 — 工厂方法不做非空校验（领域层不校验）
// ─────────────────────────────────────────────────────────────────

func TestNewPayPalTransaction_EmptyToken_CreatesSuccessfully(t *testing.T) {
	// AC-3
	emptyToken := model.PayPalToken{OrderID: "", PayerID: ""}
	txn := model.NewPayPalTransaction("user-1", "prod-1", validMoney(), emptyToken)

	// 工厂方法不校验字段非空，创建成功
	if txn == nil {
		t.Fatal("want non-nil txn, got nil")
	}
	if txn.PayPalToken.OrderID != "" || txn.PayPalToken.PayerID != "" {
		t.Error("empty token fields should be stored as empty strings")
	}
	if txn.Status != model.StatusCreated {
		t.Errorf("Status: want CREATED, got %s", txn.Status)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-4: PayPal 交易 MarkAuthorized — 正常转换
// ─────────────────────────────────────────────────────────────────

func TestPaymentTransaction_MarkAuthorized_PayPal_Succeeds(t *testing.T) {
	// AC-4
	txn := newPayPalTxn()
	before := time.Now()
	err := txn.MarkAuthorized("CAPTURE-123", "")
	after := time.Now()

	if err != nil {
		t.Fatalf("MarkAuthorized: want nil error, got %v", err)
	}

	// Status 转换正确
	if txn.Status != model.StatusAuthorized {
		t.Errorf("Status: want AUTHORIZED, got %s", txn.Status)
	}

	// ProviderRef 写入
	if txn.ProviderRef != "CAPTURE-123" {
		t.Errorf("ProviderRef: want CAPTURE-123, got %s", txn.ProviderRef)
	}

	// AuthorizedAt 不为 nil，且在调用区间内
	if txn.AuthorizedAt == nil {
		t.Fatal("AuthorizedAt must not be nil after MarkAuthorized")
	}
	if txn.AuthorizedAt.Before(before) || txn.AuthorizedAt.After(after) {
		t.Errorf("AuthorizedAt out of expected range: %v", txn.AuthorizedAt)
	}

	// Events 包含 1 个 PaymentAuthorized 事件
	if len(txn.Events) != 1 {
		t.Fatalf("Events: want 1, got %d", len(txn.Events))
	}
	authorized, ok := txn.Events[0].(event.PaymentAuthorized)
	if !ok {
		t.Fatalf("Events[0]: want PaymentAuthorized, got %T", txn.Events[0])
	}
	if authorized.TransactionID != string(txn.ID) {
		t.Errorf("PaymentAuthorized.TransactionID: want %s, got %s", txn.ID, authorized.TransactionID)
	}
	if authorized.Amount != txn.Amount.Amount {
		t.Errorf("PaymentAuthorized.Amount: want %d, got %d", txn.Amount.Amount, authorized.Amount)
	}
	if authorized.Currency != txn.Amount.Currency {
		t.Errorf("PaymentAuthorized.Currency: want %s, got %s", txn.Amount.Currency, authorized.Currency)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-5: PayPal 交易 MarkAuthorized — 非法状态转换（重复授权）
// ─────────────────────────────────────────────────────────────────

func TestPaymentTransaction_MarkAuthorized_PayPal_AlreadyAuthorized_ReturnsError(t *testing.T) {
	// AC-5
	txn := newPayPalTxnAuthorized()
	eventCountBefore := len(txn.Events)

	err := txn.MarkAuthorized("CAPTURE-456", "")

	if err == nil {
		t.Fatal("want ErrInvalidStateTransition, got nil")
	}
	if err != model.ErrInvalidStateTransition {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}

	// Status 不变
	if txn.Status != model.StatusAuthorized {
		t.Errorf("Status must remain AUTHORIZED, got %s", txn.Status)
	}

	// 无新事件产生
	if len(txn.Events) != eventCountBefore {
		t.Errorf("Events count must not change: before %d, after %d", eventCountBefore, len(txn.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-6: PayPal 交易 MarkCaptured — 正常转换
// ─────────────────────────────────────────────────────────────────

func TestPaymentTransaction_MarkCaptured_PayPal_Succeeds(t *testing.T) {
	// AC-6
	txn := newPayPalTxnAuthorized()
	txn.ClearEvents() // 清空 Authorized 事件，便于验证 Captured 事件
	before := time.Now()
	err := txn.MarkCaptured()
	after := time.Now()

	if err != nil {
		t.Fatalf("MarkCaptured: want nil error, got %v", err)
	}

	if txn.Status != model.StatusCaptured {
		t.Errorf("Status: want CAPTURED, got %s", txn.Status)
	}

	if txn.CapturedAt == nil {
		t.Fatal("CapturedAt must not be nil after MarkCaptured")
	}
	if txn.CapturedAt.Before(before) || txn.CapturedAt.After(after) {
		t.Errorf("CapturedAt out of expected range: %v", txn.CapturedAt)
	}

	if len(txn.Events) != 1 {
		t.Fatalf("Events: want 1, got %d", len(txn.Events))
	}
	captured, ok := txn.Events[0].(event.PaymentCaptured)
	if !ok {
		t.Fatalf("Events[0]: want PaymentCaptured, got %T", txn.Events[0])
	}
	if captured.TransactionID != string(txn.ID) {
		t.Errorf("PaymentCaptured.TransactionID: want %s, got %s", txn.ID, captured.TransactionID)
	}
	if captured.Amount != txn.Amount.Amount {
		t.Errorf("PaymentCaptured.Amount: want %d, got %d", txn.Amount.Amount, captured.Amount)
	}
	if captured.Currency != txn.Amount.Currency {
		t.Errorf("PaymentCaptured.Currency: want %s, got %s", txn.Amount.Currency, captured.Currency)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-7: PayPal 交易 MarkCaptured — 非法状态转换（Created 直接 Capture）
// ─────────────────────────────────────────────────────────────────

func TestPaymentTransaction_MarkCaptured_PayPal_FromCreated_ReturnsError(t *testing.T) {
	// AC-7
	txn := newPayPalTxn() // Status=CREATED

	err := txn.MarkCaptured()

	if err != model.ErrInvalidStateTransition {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
	if txn.Status != model.StatusCreated {
		t.Errorf("Status must remain CREATED, got %s", txn.Status)
	}
	if len(txn.Events) != 0 {
		t.Errorf("Events must be empty, got %d", len(txn.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-8: PayPal 交易 MarkRefunded — 正常转换
// ─────────────────────────────────────────────────────────────────

func TestPaymentTransaction_MarkRefunded_PayPal_Succeeds(t *testing.T) {
	// AC-8
	txn := newPayPalTxnCaptured()
	txn.ClearEvents() // 清空 Captured 事件
	before := time.Now()
	err := txn.MarkRefunded()
	after := time.Now()

	if err != nil {
		t.Fatalf("MarkRefunded: want nil error, got %v", err)
	}

	if txn.Status != model.StatusRefunded {
		t.Errorf("Status: want REFUNDED, got %s", txn.Status)
	}

	if txn.RefundedAt == nil {
		t.Fatal("RefundedAt must not be nil after MarkRefunded")
	}
	if txn.RefundedAt.Before(before) || txn.RefundedAt.After(after) {
		t.Errorf("RefundedAt out of expected range: %v", txn.RefundedAt)
	}

	if len(txn.Events) != 1 {
		t.Fatalf("Events: want 1, got %d", len(txn.Events))
	}
	refunded, ok := txn.Events[0].(event.PaymentRefunded)
	if !ok {
		t.Fatalf("Events[0]: want PaymentRefunded, got %T", txn.Events[0])
	}
	if refunded.TransactionID != string(txn.ID) {
		t.Errorf("PaymentRefunded.TransactionID mismatch")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-9: PayPal 交易 MarkRefunded — 非法状态转换（Authorized 直接 Refund）
// ─────────────────────────────────────────────────────────────────

func TestPaymentTransaction_MarkRefunded_PayPal_FromAuthorized_ReturnsError(t *testing.T) {
	// AC-9
	txn := newPayPalTxnAuthorized()
	eventCountBefore := len(txn.Events)

	err := txn.MarkRefunded()

	if err != model.ErrInvalidStateTransition {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
	if txn.Status != model.StatusAuthorized {
		t.Errorf("Status must remain AUTHORIZED, got %s", txn.Status)
	}
	if len(txn.Events) != eventCountBefore {
		t.Errorf("Events count must not change: before %d, after %d", eventCountBefore, len(txn.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-10: PayPal 交易 MarkFailed — 任意状态可标记失败，不产生事件
// ─────────────────────────────────────────────────────────────────

func TestPaymentTransaction_MarkFailed_PayPal_FromCreated_SetsStatusAndReason(t *testing.T) {
	// AC-10
	txn := newPayPalTxn()

	txn.MarkFailed("paypal token invalid")

	if txn.Status != model.StatusFailed {
		t.Errorf("Status: want FAILED, got %s", txn.Status)
	}
	if txn.FailReason != "paypal token invalid" {
		t.Errorf("FailReason: want 'paypal token invalid', got %s", txn.FailReason)
	}
	// MarkFailed 不产生领域事件
	if len(txn.Events) != 0 {
		t.Errorf("Events must be empty after MarkFailed, got %d", len(txn.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-11: ClearEvents — 返回并清空 PayPal 事件，二次调用返回空
// ─────────────────────────────────────────────────────────────────

func TestPaymentTransaction_ClearEvents_PayPal_ReturnsAndClearsEvents(t *testing.T) {
	// AC-11
	txn := newPayPalTxnAuthorized() // Events 含 1 个 PaymentAuthorized

	// 第一次 ClearEvents
	events := txn.ClearEvents()
	if len(events) != 1 {
		t.Fatalf("ClearEvents: want 1 event, got %d", len(events))
	}
	if _, ok := events[0].(event.PaymentAuthorized); !ok {
		t.Errorf("events[0]: want PaymentAuthorized, got %T", events[0])
	}

	// 调用后 Events 为空
	if len(txn.Events) != 0 {
		t.Errorf("txn.Events must be empty after ClearEvents, got %d", len(txn.Events))
	}

	// 第二次 ClearEvents 返回空切片
	events2 := txn.ClearEvents()
	if len(events2) != 0 {
		t.Errorf("second ClearEvents: want 0 events, got %d", len(events2))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-12: 新增错误变量已定义且语义唯一
// ─────────────────────────────────────────────────────────────────

func TestErrors_PayPal_AreDefinedAndUnique(t *testing.T) {
	// AC-12
	if model.ErrPayPalTokenInvalid == nil {
		t.Error("ErrPayPalTokenInvalid must not be nil")
	}
	if model.ErrPayPalOrderMismatch == nil {
		t.Error("ErrPayPalOrderMismatch must not be nil")
	}
	if model.ErrPayPalTokenInvalid == model.ErrPayPalOrderMismatch {
		t.Error("ErrPayPalTokenInvalid and ErrPayPalOrderMismatch must not be equal")
	}
	if model.ErrPayPalTokenInvalid == model.ErrAuthorizationDeclined {
		t.Error("ErrPayPalTokenInvalid must not equal ErrAuthorizationDeclined")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-15: NewPayPalTransaction 与 NewPaymentTransaction 的 Method 字段不同
// ─────────────────────────────────────────────────────────────────

func TestNewPayPalTransaction_vs_NewPaymentTransaction_DifferentMethod(t *testing.T) {
	// AC-15
	cardToken := model.CardToken{TokenID: "tok_visa", Last4: "4242", Brand: "Visa"}
	cardTxn := model.NewPaymentTransaction("user-1", "prod-1", validMoney(), cardToken)
	paypalTxn := model.NewPayPalTransaction("user-1", "prod-1", validMoney(), validPayPalToken())

	if cardTxn.Method != model.PaymentMethodCard {
		t.Errorf("Card txn Method: want CARD, got %s", cardTxn.Method)
	}
	if paypalTxn.Method != model.PaymentMethodPayPal {
		t.Errorf("PayPal txn Method: want PAYPAL, got %s", paypalTxn.Method)
	}
	if cardTxn.Method == paypalTxn.Method {
		t.Error("Card and PayPal transactions must have different Method values")
	}

	// CardToken / PayPalToken 互不干扰
	if paypalTxn.CardToken.TokenID != "" {
		t.Error("PayPal txn must have zero CardToken")
	}
	if cardTxn.PayPalToken.OrderID != "" {
		t.Error("Card txn must have zero PayPalToken")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-14: PayPalAuthResult DTO 字段可正确读写
// ─────────────────────────────────────────────────────────────────

func TestPayPalToken_ValueObject_FieldsReadable(t *testing.T) {
	// AC-14（在 domain 层验证值对象本身）
	token := model.PayPalToken{
		OrderID: "5O190127TN364715T",
		PayerID: "FSMVU44LF3YUS",
	}
	if token.OrderID != "5O190127TN364715T" {
		t.Errorf("OrderID: want 5O190127TN364715T, got %s", token.OrderID)
	}
	if token.PayerID != "FSMVU44LF3YUS" {
		t.Errorf("PayerID: want FSMVU44LF3YUS, got %s", token.PayerID)
	}
}

// ─────────────────────────────────────────────────────────────────
// 完整状态机路径测试（Created → Authorized → Captured → Refunded）
// ─────────────────────────────────────────────────────────────────

func TestPaymentTransaction_PayPal_FullStateMachineTransition(t *testing.T) {
	txn := newPayPalTxn()

	// Created → Authorized
	if err := txn.MarkAuthorized("CAPTURE-999", ""); err != nil {
		t.Fatalf("MarkAuthorized: %v", err)
	}
	if txn.Status != model.StatusAuthorized {
		t.Errorf("after MarkAuthorized: want AUTHORIZED, got %s", txn.Status)
	}

	txn.ClearEvents()

	// Authorized → Captured
	if err := txn.MarkCaptured(); err != nil {
		t.Fatalf("MarkCaptured: %v", err)
	}
	if txn.Status != model.StatusCaptured {
		t.Errorf("after MarkCaptured: want CAPTURED, got %s", txn.Status)
	}

	txn.ClearEvents()

	// Captured → Refunded
	if err := txn.MarkRefunded(); err != nil {
		t.Fatalf("MarkRefunded: %v", err)
	}
	if txn.Status != model.StatusRefunded {
		t.Errorf("after MarkRefunded: want REFUNDED, got %s", txn.Status)
	}
}
