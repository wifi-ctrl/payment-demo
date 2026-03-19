package port

import "context"

// TaxRateQuery payment 对税率配置的查询端口（消费方定义）。
type TaxRateQuery interface {
	// FindTaxRate 按商品 ID 和货币查询税率（basis point）。
	// 如 1000 = 10.00%，0 = 免税。
	FindTaxRate(ctx context.Context, productID string, currency string) (int64, error)
}
