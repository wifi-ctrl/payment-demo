package model

import "payment-demo/internal/shared/money"

type Money = money.Money

func NewMoney(amount int64, currency string) Money {
	return money.NewMoney(amount, currency)
}
