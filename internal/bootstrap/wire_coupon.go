package bootstrap

import (
	couponInmem "payment-demo/internal/coupon/adapter/inmem"
	couponApp "payment-demo/internal/coupon/application"
	couponHTTP "payment-demo/internal/coupon/handler/http"
)

// CouponModule coupon 上下文的组装产物。
// CouponRepo 共享给 payment ACL adapter（CouponAdapter）。
type CouponModule struct {
	Handler    *couponHTTP.CouponHandler
	CouponRepo *couponInmem.InMemoryCouponRepository
}

func wireCoupon() *CouponModule {
	repo := couponInmem.NewInMemoryCouponRepository()
	uc := couponApp.NewCouponUseCase(repo)
	return &CouponModule{
		Handler:    couponHTTP.NewCouponHandler(uc),
		CouponRepo: repo,
	}
}
