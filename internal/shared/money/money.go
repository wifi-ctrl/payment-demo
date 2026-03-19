// Package money 提供跨上下文共享的金额值对象。
// 各上下文统一使用此包，避免重复定义 Money（解决 P3 问题）。
package money

import (
	"errors"
	"fmt"
)

// ErrCurrencyMismatch 币种不一致
var ErrCurrencyMismatch = errors.New("currency mismatch")

// ErrNegativeAmount 计算结果为负数
var ErrNegativeAmount = errors.New("amount cannot be negative")

// Money 金额值对象，不可变。
// Amount 以最小货币单位存储（如 USD cents、CNY fen）。
// Currency 遵循 ISO 4217（如 "USD"、"CNY"）。
type Money struct {
	Amount   int64  // 最小货币单位（如 cents）
	Currency string // ISO 4217，如 "USD"
}

// NewMoney 构造 Money 值对象。
func NewMoney(amount int64, currency string) Money {
	return Money{Amount: amount, Currency: currency}
}

// String 可读表示。
func (m Money) String() string {
	return fmt.Sprintf("%d %s", m.Amount, m.Currency)
}

// GreaterThan 比较：m > other（须同币种，否则 panic）。
func (m Money) GreaterThan(other Money) bool {
	if m.Currency != other.Currency {
		panic("currency mismatch in GreaterThan")
	}
	return m.Amount > other.Amount
}

// Add 加法，须同币种，否则 panic（调用方保证币种一致）。
func (m Money) Add(other Money) Money {
	if m.Currency != other.Currency {
		panic("currency mismatch in Add")
	}
	return Money{Amount: m.Amount + other.Amount, Currency: m.Currency}
}

// Subtract 减法，结果不可为负，否则返回 ErrNegativeAmount。
// 须同币种，币种不符返回 ErrCurrencyMismatch。
func (m Money) Subtract(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, ErrCurrencyMismatch
	}
	if m.Amount < other.Amount {
		return Money{}, ErrNegativeAmount
	}
	return Money{Amount: m.Amount - other.Amount, Currency: m.Currency}, nil
}

// IsZero 金额是否为零。
func (m Money) IsZero() bool { return m.Amount == 0 }

// Equals 值相等判断（Amount 和 Currency 均相等）。
func (m Money) Equals(other Money) bool {
	return m.Amount == other.Amount && m.Currency == other.Currency
}

// MultiplyBasisPoint 按 basis point 比例计算金额，结果向下取整。
// bp = 1000 表示 10.00%，bp = 500 表示 5.00%。
// 用于税额计算（taxAmount = amount × taxRate / 10000）
// 和百分比折扣计算（discount = amount × discountRate / 10000）。
// 结果为负时返回 ErrNegativeAmount（bp 为负时发生）。
func (m Money) MultiplyBasisPoint(bp int64) (Money, error) {
	if bp < 0 {
		return Money{}, ErrNegativeAmount
	}
	result := m.Amount * bp / 10000
	return Money{Amount: result, Currency: m.Currency}, nil
}
