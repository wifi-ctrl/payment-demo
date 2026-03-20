package model

import "errors"

var (
	ErrOrderNotFound        = errors.New("order not found")
	ErrInvalidStateTransition = errors.New("invalid order state transition")
	ErrProductNotFound      = errors.New("product not found")
	ErrProductNotActive     = errors.New("product is not active")
	ErrMerchantRequired     = errors.New("merchant_id is required")
	ErrPaymentFailed        = errors.New("payment authorization failed")
)
