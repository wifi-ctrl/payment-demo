package port

import "context"

// CatalogQuery 商品目录查询端口（被驱动端口）
// payment 对 catalog 上下文的 ACL 边界
// 注意：ProductView 是 payment 自己定义的视图，不是 catalog 的 Product
type CatalogQuery interface {
	FindProduct(ctx context.Context, productID string) (*ProductView, error)
}

// ProductView payment 视角的商品视图 — 只包含 payment 需要的字段
// 与 catalog 的 Product 模型解耦
type ProductView struct {
	ID       string
	Name     string
	Amount   int64
	Currency string
	IsActive bool
}
