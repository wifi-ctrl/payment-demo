package model

import "errors"

var (
	// ErrMerchantNotFound 按 ID 查询商户时不存在。
	ErrMerchantNotFound = errors.New("merchant not found")

	// ErrMerchantNotActive 商户非 ACTIVE 状态时执行受限操作。
	ErrMerchantNotActive = errors.New("merchant is not active")

	// ErrCredentialNotFound 指定渠道/凭据 ID 的凭据不存在。
	ErrCredentialNotFound = errors.New("channel credential not found")

	// ErrCredentialAlreadyExists 该渠道已存在 ACTIVE 凭据，须先吊销旧凭据。
	ErrCredentialAlreadyExists = errors.New("active credential already exists for this channel")

	// ErrInvalidStateTransition 非法状态流转（如已 REVOKED 的凭据再次吊销）。
	ErrInvalidStateTransition = errors.New("invalid state transition")
)
