package model

// CardToken 卡令牌值对象，由前端 tokenization 生成
type CardToken struct {
	TokenID string // 如 "tok_visa_4242"
	Last4   string // 卡号后四位
	Brand   string // Visa / Mastercard / ...
}
