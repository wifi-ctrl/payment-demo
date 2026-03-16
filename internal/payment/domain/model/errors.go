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
	ErrCardNotFound  = errors.New("card not found")
	ErrCardNotUsable = errors.New("card is not usable")

	// PayPal 专属错误
	ErrPayPalTokenInvalid  = errors.New("paypal token is invalid or expired")
	ErrPayPalOrderMismatch = errors.New("paypal order amount mismatch")

	// 路由错误
	ErrUnsupportedPaymentMethod = errors.New("unsupported payment method")

	// 多商户相关错误
	// ErrMerchantRequired 请求未提供 merchant_id。
	ErrMerchantRequired = errors.New("merchant_id is required")
	// ErrMerchantGatewayBuildFailed 按商户凭据构建 Gateway 失败（凭据格式错误等）。
	ErrMerchantGatewayBuildFailed = errors.New("failed to build gateway for merchant")
)
