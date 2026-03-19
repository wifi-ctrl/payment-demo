package model

import "payment-demo/internal/shared/money"

// Money 金额值对象，复用 Shared Kernel（type alias，与 money.Money 同一类型）。
type Money = money.Money

// NewMoney 构造 Money 值对象。
func NewMoney(amount int64, currency string) Money {
	return money.NewMoney(amount, currency)
}
