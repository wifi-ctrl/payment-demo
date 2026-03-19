package model

// EncryptedPAN AES-256-GCM 加密后的 PAN 密文（含 nonce + ciphertext + authTag）
type EncryptedPAN struct {
	Ciphertext []byte // nonce(12) + ciphertext + authTag(16)
	KeyVersion int    // 加密时使用的 DEK 版本号
}

// PANHash HMAC-SHA-256(hmac_key, PAN) 的不可逆哈希，查重用
type PANHash string

// MaskedPAN 脱敏卡号
type MaskedPAN string

// CardBrand 卡品牌
type CardBrand string

const (
	CardBrandVisa       CardBrand = "visa"
	CardBrandMastercard CardBrand = "mastercard"
	CardBrandUnionPay   CardBrand = "unionpay"
	CardBrandUnknown    CardBrand = "unknown"
)

// Expiry 有效期值对象
type Expiry struct {
	Month int // 1-12
	Year  int // 如 2028
}

// RawCardData 令牌化阶段的原始卡数据（仅在内存中短暂存在）
type RawCardData struct {
	PAN            string
	ExpiryMonth    int
	ExpiryYear     int
	CVV            string
	CardholderName string
}

// String 防止意外 %v 打印泄露 PAN/CVV
func (r RawCardData) String() string   { return "[REDACTED]" }
func (r RawCardData) GoString() string { return "[REDACTED]" }
