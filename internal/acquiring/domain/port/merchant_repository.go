// Package port 定义 acquiring 上下文的被驱动端口（输出端口）。
package port

import (
	"context"

	"payment-demo/internal/acquiring/domain/model"
)

// MerchantRepository 商户仓储端口（被驱动端口）。
// 实现由 adapter/persistence 提供；InMemory 实现须用 sync.RWMutex 保护并发。
type MerchantRepository interface {
	// Save 新增或更新商户聚合根（upsert 语义）。
	Save(ctx context.Context, m *model.Merchant) error

	// FindByID 按商户 ID 查询，不存在返回 model.ErrMerchantNotFound。
	FindByID(ctx context.Context, id model.MerchantID) (*model.Merchant, error)

	// FindAll 返回所有商户列表，用于管理后台；生产环境可替换为分页版本。
	FindAll(ctx context.Context) ([]*model.Merchant, error)
}
