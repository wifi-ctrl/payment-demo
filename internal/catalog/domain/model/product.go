package model

// ProductID 商品唯一标识
type ProductID string

// ProductStatus 商品状态
type ProductStatus string

const (
	ProductStatusActive  ProductStatus = "ACTIVE"
	ProductStatusOffline ProductStatus = "OFFLINE"
)

// Product 商品聚合根
type Product struct {
	ID     ProductID
	Name   string
	Price  Money // 值对象
	Status ProductStatus
}

func (p *Product) IsActive() bool {
	return p.Status == ProductStatusActive
}

// Money 金额值对象
type Money struct {
	Amount   int64  // 最小货币单位（cents）
	Currency string // ISO 4217
}
