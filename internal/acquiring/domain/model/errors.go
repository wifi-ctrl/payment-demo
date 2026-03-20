package model

import "errors"

var (
	ErrInvalidStateTransition = errors.New("invalid state transition")

	// Merchant
	ErrMerchantNotFound        = errors.New("merchant not found")
	ErrMerchantNotActive       = errors.New("merchant is not active")
	ErrCredentialNotFound      = errors.New("channel credential not found")
	ErrCredentialAlreadyExists = errors.New("active credential already exists for this channel")

	// Transaction
	ErrTransactionNotFound    = errors.New("transaction not found")
	ErrAuthorizationDeclined  = errors.New("authorization declined by gateway")
	ErrCaptureFailure         = errors.New("capture failed at gateway")
	ErrRefundFailure          = errors.New("refund failed at gateway")
	ErrCardNotFound           = errors.New("card not found")
	ErrCardNotUsable          = errors.New("card is not usable")
	ErrPayPalTokenInvalid     = errors.New("paypal token is invalid or expired")
	ErrPayPalOrderMismatch    = errors.New("paypal order amount mismatch")
	ErrUnsupportedPaymentMethod = errors.New("unsupported payment method")
	ErrCardTokenOwnerMismatch = errors.New("card token does not belong to the current user")
	ErrTemporaryCardTokenBad  = errors.New("temporary card token is invalid or expired")
	ErrMerchantRequired       = errors.New("merchant_id is required")
	ErrMerchantGatewayBuildFailed = errors.New("failed to build gateway for merchant")
)
