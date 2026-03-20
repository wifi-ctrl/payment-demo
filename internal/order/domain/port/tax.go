package port

import "context"

type TaxRateQuery interface {
	FindTaxRate(ctx context.Context, productID string, currency string) (int64, error)
}
