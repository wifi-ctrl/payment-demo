package service

import (
	"payment-demo/internal/order/domain/model"
)

var ErrDiscountExceedsAmount = errNew("discount exceeds original amount")
var ErrNegativeFinalAmount = errNew("final amount cannot be negative")

func errNew(s string) error { return &pricingError{s} }

type pricingError struct{ msg string }

func (e *pricingError) Error() string { return e.msg }

// CalculateFinalAmount 纯计算：原价 - 折扣 + 税。
//
//	discountType: "PERCENTAGE"（basis point）或 "FIXED"（cents），空字符串 = 无折扣。
//	discountValue: PERCENTAGE 时为 basis point，FIXED 时为 cents。
//	taxBP: 税率 basis point（1000 = 10.00%），0 = 免税。
func CalculateFinalAmount(original model.Money, discountType string, discountValue int64, taxBP int64) (finalAmount, discountAmount, taxAmount model.Money, err error) {
	discountAmount = model.NewMoney(0, original.Currency)

	switch discountType {
	case "PERCENTAGE":
		discountAmount, err = original.MultiplyBasisPoint(discountValue)
		if err != nil {
			return model.Money{}, model.Money{}, model.Money{}, ErrDiscountExceedsAmount
		}
	case "FIXED":
		fixed := model.NewMoney(discountValue, original.Currency)
		if fixed.GreaterThan(original) {
			return model.Money{}, model.Money{}, model.Money{}, ErrDiscountExceedsAmount
		}
		discountAmount = fixed
	}

	afterDiscount, err := original.Subtract(discountAmount)
	if err != nil {
		return model.Money{}, model.Money{}, model.Money{}, ErrNegativeFinalAmount
	}

	taxAmount = model.NewMoney(0, original.Currency)
	if taxBP > 0 {
		taxAmount, err = afterDiscount.MultiplyBasisPoint(taxBP)
		if err != nil {
			return model.Money{}, model.Money{}, model.Money{}, err
		}
	}

	finalAmount = afterDiscount.Add(taxAmount)
	return finalAmount, discountAmount, taxAmount, nil
}
