package model

import "errors"

var (
	ErrInvalidStateTransition = errors.New("invalid state transition")
	ErrTransactionNotFound    = errors.New("transaction not found")
	ErrAuthorizationDeclined  = errors.New("authorization declined by gateway")
	ErrCaptureFailure         = errors.New("capture failed at gateway")
	ErrRefundFailure          = errors.New("refund failed at gateway")
	ErrProductNotFound        = errors.New("product not found")
	ErrProductNotActive       = errors.New("product is not active")
	// 已保存卡相关错误（通过 CardQuery ACL 传递）
	ErrCardNotFound   = errors.New("card not found")
	ErrCardNotUsable  = errors.New("card is not usable")
)
