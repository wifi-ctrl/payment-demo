// Package service 提供 payment 上下文的领域服务。
package service

import (
	"payment-demo/internal/shared/money"
)

// ErrDiscountExceedsAmount 折扣超过原始金额
var ErrDiscountExceedsAmount = errNew("discount exceeds original amount")

// ErrNegativeFinalAmount 最终价为负数
var ErrNegativeFinalAmount = errNew("final amount cannot be negative")

func errNew(s string) error { return &pricingError{s} }

type pricingError struct{ msg string }

func (e *pricingError) Error() string { return e.msg }

// CalculateFinalAmount 纯计算：原价 - 折扣 + 税。
//
//	discountType: "PERCENTAGE"（basis point）或 "FIXED"（cents），空字符串 = 无折扣。
//	discountValue: PERCENTAGE 时为 basis point，FIXED 时为 cents。
//	taxBP: 税率 basis point（1000 = 10.00%），0 = 免税。
//
// 返回 finalAmount、discountAmount、taxAmount。
func CalculateFinalAmount(original money.Money, discountType string, discountValue int64, taxBP int64) (finalAmount, discountAmount, taxAmount money.Money, err error) {
	discountAmount = money.NewMoney(0, original.Currency)

	switch discountType {
	case "PERCENTAGE":
		discountAmount, err = original.MultiplyBasisPoint(discountValue)
		if err != nil {
			return money.Money{}, money.Money{}, money.Money{}, ErrDiscountExceedsAmount
		}
	case "FIXED":
		fixed := money.NewMoney(discountValue, original.Currency)
		if fixed.GreaterThan(original) {
			return money.Money{}, money.Money{}, money.Money{}, ErrDiscountExceedsAmount
		}
		discountAmount = fixed
	}

	afterDiscount, err := original.Subtract(discountAmount)
	if err != nil {
		return money.Money{}, money.Money{}, money.Money{}, ErrNegativeFinalAmount
	}

	taxAmount = money.NewMoney(0, original.Currency)
	if taxBP > 0 {
		taxAmount, err = afterDiscount.MultiplyBasisPoint(taxBP)
		if err != nil {
			return money.Money{}, money.Money{}, money.Money{}, err
		}
	}

	finalAmount = afterDiscount.Add(taxAmount)
	return finalAmount, discountAmount, taxAmount, nil
}
