package persistence_test

import (
	"context"
	"sync"
	"testing"

	"payment-demo/internal/acquiring/adapter/persistence"
	"payment-demo/internal/acquiring/domain/model"
)

// ─────────────────────────────────────────────────────────────────
// 辅助函数
// ─────────────────────────────────────────────────────────────────

func newPayPalTransaction(userID, orderID string) *model.PaymentTransaction {
	return model.NewPayPalTransaction(
		userID,
		orderID,
		model.NewMoney(1000, "USD"),
		model.PayPalToken{OrderID: "5O190127TN364715T", PayerID: "FSMVU44LF3YUS"},
	)
}

// ─────────────────────────────────────────────────────────────────
// AC-45: Save PayPal 交易后可 FindByID，Method/PayPalToken 字段正确
// ─────────────────────────────────────────────────────────────────

func TestInMemoryTransactionRepository_SavePayPal_FindByID_CorrectFields(t *testing.T) {
	// AC-45
	repo := persistence.NewInMemoryTransactionRepository()
	ctx := context.Background()
	txn := newPayPalTransaction("user-1", "prod-1")

	if err := repo.Save(ctx, txn); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(ctx, txn.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}

	// Method 正确
	if got.Method != model.PaymentMethodPayPal {
		t.Errorf("Method: want PAYPAL, got %s", got.Method)
	}

	// PayPalToken 与 Save 时一致
	if got.PayPalToken.OrderID != "5O190127TN364715T" {
		t.Errorf("PayPalToken.OrderID: want 5O190127TN364715T, got %s", got.PayPalToken.OrderID)
	}
	if got.PayPalToken.PayerID != "FSMVU44LF3YUS" {
		t.Errorf("PayPalToken.PayerID: want FSMVU44LF3YUS, got %s", got.PayPalToken.PayerID)
	}

	// CardToken 为零值
	if got.CardToken.TokenID != "" || got.CardToken.Last4 != "" || got.CardToken.Brand != "" {
		t.Errorf("CardToken must be zero value, got %+v", got.CardToken)
	}

	// 基本字段
	if got.UserID != "user-1" {
		t.Errorf("UserID: want user-1, got %s", got.UserID)
	}
	if got.OrderID != "prod-1" {
		t.Errorf("OrderID: want prod-1, got %s", got.OrderID)
	}
	if got.Amount.Amount != 1000 || got.Amount.Currency != "USD" {
		t.Errorf("Amount: want {1000 USD}, got %+v", got.Amount)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-46: 先 Save PayPal 交易，MarkAuthorized 后再次 Save，Method 不丢失
// ─────────────────────────────────────────────────────────────────

func TestInMemoryTransactionRepository_SavePayPal_UpdateDoesNotLoseMethod(t *testing.T) {
	// AC-46
	repo := persistence.NewInMemoryTransactionRepository()
	ctx := context.Background()
	txn := newPayPalTransaction("user-1", "prod-1")

	// 第一次 Save（Status=CREATED）
	if err := repo.Save(ctx, txn); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	// 状态转换
	if err := txn.MarkAuthorized("CAPTURE-456", ""); err != nil {
		t.Fatalf("MarkAuthorized: %v", err)
	}

	// 第二次 Save（Status=AUTHORIZED）
	if err := repo.Save(ctx, txn); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	got, err := repo.FindByID(ctx, txn.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}

	// Status 更新
	if got.Status != model.StatusAuthorized {
		t.Errorf("Status: want AUTHORIZED, got %s", got.Status)
	}
	// Method 仍为 PAYPAL
	if got.Method != model.PaymentMethodPayPal {
		t.Errorf("Method: want PAYPAL after update, got %s", got.Method)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-47: 并发 Save — 10 个 goroutine 并发 Save 不同 PayPal 交易，无 race condition
// ─────────────────────────────────────────────────────────────────

func TestInMemoryTransactionRepository_ConcurrentSavePayPal_NoRace(t *testing.T) {
	// AC-47（需配合 go test -race 执行）
	repo := persistence.NewInMemoryTransactionRepository()
	ctx := context.Background()
	const goroutines = 10

	txns := make([]*model.PaymentTransaction, goroutines)
	for i := 0; i < goroutines; i++ {
		txns[i] = newPayPalTransaction("user-concurrent", "prod-1")
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(txn *model.PaymentTransaction) {
			defer wg.Done()
			_ = repo.Save(ctx, txn)
		}(txns[i])
	}
	wg.Wait()

	// 所有 10 笔交易均可通过 FindByID 取回
	for i, txn := range txns {
		got, err := repo.FindByID(ctx, txn.ID)
		if err != nil {
			t.Errorf("goroutine[%d] FindByID: %v", i, err)
			continue
		}
		if got.Method != model.PaymentMethodPayPal {
			t.Errorf("goroutine[%d] Method: want PAYPAL, got %s", i, got.Method)
		}
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-15: Card 和 PayPal 交易可共存于同一仓储，互不干扰
// ─────────────────────────────────────────────────────────────────

func TestInMemoryTransactionRepository_CardAndPayPal_Coexist(t *testing.T) {
	// AC-15
	repo := persistence.NewInMemoryTransactionRepository()
	ctx := context.Background()

	cardTxn := model.NewPaymentTransaction(
		"user-1", "prod-1",
		model.NewMoney(2000, "USD"),
		model.CardToken{TokenID: "tok_visa", Last4: "4242", Brand: "Visa"},
	)
	paypalTxn := newPayPalTransaction("user-2", "prod-2")

	if err := repo.Save(ctx, cardTxn); err != nil {
		t.Fatalf("Save card txn: %v", err)
	}
	if err := repo.Save(ctx, paypalTxn); err != nil {
		t.Fatalf("Save paypal txn: %v", err)
	}

	// 查 Card 交易
	gotCard, err := repo.FindByID(ctx, cardTxn.ID)
	if err != nil {
		t.Fatalf("FindByID card: %v", err)
	}
	if gotCard.Method != model.PaymentMethodCard {
		t.Errorf("card txn Method: want CARD, got %s", gotCard.Method)
	}

	// 查 PayPal 交易
	gotPayPal, err := repo.FindByID(ctx, paypalTxn.ID)
	if err != nil {
		t.Fatalf("FindByID paypal: %v", err)
	}
	if gotPayPal.Method != model.PaymentMethodPayPal {
		t.Errorf("paypal txn Method: want PAYPAL, got %s", gotPayPal.Method)
	}
}

// ─────────────────────────────────────────────────────────────────
// 补充：FindByID 不存在的 PayPal 交易返回 ErrTransactionNotFound
// ─────────────────────────────────────────────────────────────────

func TestInMemoryTransactionRepository_FindByID_NotFound_ReturnsError(t *testing.T) {
	repo := persistence.NewInMemoryTransactionRepository()
	ctx := context.Background()

	_, err := repo.FindByID(ctx, model.TransactionID("nonexistent-paypal-id"))
	if err != model.ErrTransactionNotFound {
		t.Errorf("want ErrTransactionNotFound, got %v", err)
	}
}
