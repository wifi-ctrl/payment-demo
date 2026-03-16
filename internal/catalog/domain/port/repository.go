package port

import (
	"context"

	"payment-demo/internal/catalog/domain/model"
)

// ProductRepository 商品仓储端口
type ProductRepository interface {
	FindByID(ctx context.Context, id model.ProductID) (*model.Product, error)
	FindAll(ctx context.Context) ([]*model.Product, error)
}
