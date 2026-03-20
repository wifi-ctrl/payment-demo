package tax

import (
	"context"

	"payment-demo/internal/order/domain/port"
)

type StaticTaxQuery struct {
	defaultBP int64
	overrides map[string]int64
}

var _ port.TaxRateQuery = (*StaticTaxQuery)(nil)

func NewStaticTaxQuery(defaultBP int64) *StaticTaxQuery {
	return &StaticTaxQuery{
		defaultBP: defaultBP,
		overrides: make(map[string]int64),
	}
}

func (q *StaticTaxQuery) WithOverride(productID string, bp int64) *StaticTaxQuery {
	q.overrides[productID] = bp
	return q
}

func (q *StaticTaxQuery) FindTaxRate(_ context.Context, productID string, _ string) (int64, error) {
	if bp, ok := q.overrides[productID]; ok {
		return bp, nil
	}
	return q.defaultBP, nil
}
