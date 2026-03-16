package model

import "fmt"

// Money 金额值对象，不可变
type Money struct {
	Amount   int64  // 最小货币单位（如 cents）
	Currency string // ISO 4217，如 "USD"
}

func NewMoney(amount int64, currency string) Money {
	return Money{Amount: amount, Currency: currency}
}

func (m Money) String() string {
	return fmt.Sprintf("%d %s", m.Amount, m.Currency)
}

func (m Money) GreaterThan(other Money) bool {
	return m.Amount > other.Amount
}
