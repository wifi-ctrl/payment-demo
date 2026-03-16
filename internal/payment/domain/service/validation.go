package service

import "payment-demo/internal/payment/domain/model"

// ValidateCapturable 校验交易是否可扣款
func ValidateCapturable(txn *model.PaymentTransaction) error {
	if txn.Status != model.StatusAuthorized {
		return model.ErrInvalidStateTransition
	}
	return nil
}

// ValidateRefundable 校验交易是否可退款
func ValidateRefundable(txn *model.PaymentTransaction) error {
	if txn.Status != model.StatusCaptured {
		return model.ErrInvalidStateTransition
	}
	return nil
}
