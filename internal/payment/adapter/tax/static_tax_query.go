// Package tax 提供 TaxRateQuery 的静态配置实现。
package tax

import (
	"context"

	"payment-demo/internal/payment/domain/port"
)

// StaticTaxQuery 静态税率配置实现。
type StaticTaxQuery struct {
	defaultBP int64
	overrides map[string]int64
}

var _ port.TaxRateQuery = (*StaticTaxQuery)(nil)

// NewStaticTaxQuery 构造函数，defaultBP 为全局默认税率（basis point）。
func NewStaticTaxQuery(defaultBP int64) *StaticTaxQuery {
	return &StaticTaxQuery{
		defaultBP: defaultBP,
		overrides: make(map[string]int64),
	}
}

// WithOverride 注册商品级别税率覆盖。
func (q *StaticTaxQuery) WithOverride(productID string, bp int64) *StaticTaxQuery {
	q.overrides[productID] = bp
	return q
}

// FindTaxRate 返回税率（basis point）。
func (q *StaticTaxQuery) FindTaxRate(_ context.Context, productID string, _ string) (int64, error) {
	if bp, ok := q.overrides[productID]; ok {
		return bp, nil
	}
	return q.defaultBP, nil
}
