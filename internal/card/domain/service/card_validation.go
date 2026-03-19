package service

import "payment-demo/internal/card/domain/model"

// LuhnCheck Luhn 校验算法
func LuhnCheck(pan string) bool {
	sum := 0
	alt := false
	for i := len(pan) - 1; i >= 0; i-- {
		n := int(pan[i] - '0')
		if n < 0 || n > 9 {
			return false
		}
		if alt {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		alt = !alt
	}
	return sum%10 == 0
}

// IdentifyBrand 根据 BIN 识别卡品牌
func IdentifyBrand(pan string) model.CardBrand {
	if len(pan) < 2 {
		return model.CardBrandUnknown
	}
	switch {
	case pan[0] == '4':
		return model.CardBrandVisa
	case pan[:2] >= "51" && pan[:2] <= "55":
		return model.CardBrandMastercard
	case pan[:2] == "62":
		return model.CardBrandUnionPay
	default:
		return model.CardBrandUnknown
	}
}
