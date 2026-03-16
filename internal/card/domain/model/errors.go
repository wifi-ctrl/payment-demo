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
)
