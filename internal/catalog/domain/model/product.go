package model

import "payment-demo/internal/shared/money"

// ProductID 商品唯一标识
type ProductID string

// Money 金额值对象，复用 Shared Kernel。
type Money = money.Money

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
