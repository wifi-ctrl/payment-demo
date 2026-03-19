package model

import "errors"

var (
	// ErrInvalidStateTransition 非法状态转换
	ErrInvalidStateTransition = errors.New("invalid state transition")

	// ErrCardNotFound 卡不存在
	ErrCardNotFound = errors.New("card not found")

	// ErrCardNotUsable 卡不可用（非 Active 状态）
	ErrCardNotUsable = errors.New("card is not usable")

	// ErrCardBelongsToOtherUser 卡归属其他用户
	ErrCardBelongsToOtherUser = errors.New("card belongs to another user")

	// ErrVaultTokenizeFailed Vault 令牌化失败
	ErrVaultTokenizeFailed = errors.New("vault tokenize failed")

	// ErrDuplicateCard 同一用户重复绑同一张卡
	ErrDuplicateCard = errors.New("card already exists for this user")

	// ErrEncryptionFailed PAN 加密失败
	ErrEncryptionFailed = errors.New("PAN encryption failed")

	// ErrDecryptionFailed PAN 解密失败
	ErrDecryptionFailed = errors.New("PAN decryption failed")

	// ErrCardTokenExpired 临时令牌过期或已消费
	ErrCardTokenExpired = errors.New("card token expired or already consumed")

	// ErrCardTokenInvalid 无效临时令牌
	ErrCardTokenInvalid = errors.New("invalid card token")
)
