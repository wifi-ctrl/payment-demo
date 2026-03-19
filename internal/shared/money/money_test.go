package money_test

import (
	"testing"

	"payment-demo/internal/shared/money"
)

// ─────────────────────────────────────────────────────────────────
// AC-1: MultiplyBasisPoint 正常计算
// ─────────────────────────────────────────────────────────────────

func TestMoney_MultiplyBasisPoint_Normal(t *testing.T) {
	// AC-1
	m := money.NewMoney(10000, "USD")
	result, err := m.MultiplyBasisPoint(1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Amount != 1000 {
		t.Errorf("expected Amount=1000, got %d", result.Amount)
	}
	if result.Currency != "USD" {
		t.Errorf("expected Currency=USD, got %s", result.Currency)
	}
	// 原 Money 不变
	if m.Amount != 10000 {
		t.Errorf("original Money mutated: expected 10000, got %d", m.Amount)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-2: MultiplyBasisPoint 税率为零
// ─────────────────────────────────────────────────────────────────

func TestMoney_MultiplyBasisPoint_ZeroRate(t *testing.T) {
	// AC-2
	m := money.NewMoney(5000, "USD")
	result, err := m.MultiplyBasisPoint(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Amount != 0 {
		t.Errorf("expected Amount=0, got %d", result.Amount)
	}
	if result.Currency != "USD" {
		t.Errorf("expected Currency=USD, got %s", result.Currency)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-3: MultiplyBasisPoint 满额税率（10000 bp = 100%）
// ─────────────────────────────────────────────────────────────────

func TestMoney_MultiplyBasisPoint_FullRate(t *testing.T) {
	// AC-3
	m := money.NewMoney(200, "CNY")
	result, err := m.MultiplyBasisPoint(10000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Amount != 200 {
		t.Errorf("expected Amount=200, got %d", result.Amount)
	}
	if result.Currency != "CNY" {
		t.Errorf("expected Currency=CNY, got %s", result.Currency)
	}
}

// ─────────────────────────────────────────────────────────────────
// 额外：负 bp 返回错误
// ─────────────────────────────────────────────────────────────────

func TestMoney_MultiplyBasisPoint_NegativeBP(t *testing.T) {
	m := money.NewMoney(1000, "USD")
	_, err := m.MultiplyBasisPoint(-1)
	if err == nil {
		t.Error("expected error for negative basis point, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────
// NewMoney / Equals / IsZero
// ─────────────────────────────────────────────────────────────────

func TestMoney_NewMoney_Fields(t *testing.T) {
	m := money.NewMoney(100, "USD")
	if m.Amount != 100 || m.Currency != "USD" {
		t.Errorf("unexpected fields: %+v", m)
	}
}

func TestMoney_Equals(t *testing.T) {
	a := money.NewMoney(100, "USD")
	b := money.NewMoney(100, "USD")
	c := money.NewMoney(200, "USD")
	d := money.NewMoney(100, "CNY")

	if !a.Equals(b) {
		t.Error("expected a == b")
	}
	if a.Equals(c) {
		t.Error("expected a != c (different amount)")
	}
	if a.Equals(d) {
		t.Error("expected a != d (different currency)")
	}
}

func TestMoney_IsZero(t *testing.T) {
	z := money.NewMoney(0, "USD")
	nz := money.NewMoney(1, "USD")
	if !z.IsZero() {
		t.Error("expected IsZero() = true for 0")
	}
	if nz.IsZero() {
		t.Error("expected IsZero() = false for 1")
	}
}

func TestMoney_Add(t *testing.T) {
	a := money.NewMoney(100, "USD")
	b := money.NewMoney(50, "USD")
	got := a.Add(b)
	if got.Amount != 150 {
		t.Errorf("expected 150, got %d", got.Amount)
	}
}

func TestMoney_Subtract_Success(t *testing.T) {
	a := money.NewMoney(100, "USD")
	b := money.NewMoney(40, "USD")
	got, err := a.Subtract(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Amount != 60 {
		t.Errorf("expected 60, got %d", got.Amount)
	}
}

func TestMoney_Subtract_Negative(t *testing.T) {
	a := money.NewMoney(10, "USD")
	b := money.NewMoney(20, "USD")
	_, err := a.Subtract(b)
	if err == nil {
		t.Error("expected ErrNegativeAmount, got nil")
	}
}

func TestMoney_Subtract_CurrencyMismatch(t *testing.T) {
	a := money.NewMoney(100, "USD")
	b := money.NewMoney(50, "CNY")
	_, err := a.Subtract(b)
	if err == nil {
		t.Error("expected ErrCurrencyMismatch, got nil")
	}
}
