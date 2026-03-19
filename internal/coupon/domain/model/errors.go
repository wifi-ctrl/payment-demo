package model

import "errors"

var (
	// ErrCouponNotFound 优惠券不存在
	ErrCouponNotFound = errors.New("coupon not found")

	// ErrCouponNotApplicable 优惠券已过期、已用尽或状态非 ACTIVE
	ErrCouponNotApplicable = errors.New("coupon is expired, exhausted or inactive")

	// ErrCouponCodeConflict 优惠券编码已存在
	ErrCouponCodeConflict = errors.New("coupon code already exists")
)
