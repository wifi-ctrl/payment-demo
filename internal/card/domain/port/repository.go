package port

import (
	"context"

	"payment-demo/internal/card/domain/model"
)

// CardRepository 卡仓储端口（被驱动端口）
type CardRepository interface {
	// Save 新增或更新（upsert 语义）
	Save(ctx context.Context, card *model.SavedCard) error

	// FindByID 按卡 ID 查询，不存在返回 model.ErrCardNotFound
	FindByID(ctx context.Context, id model.SavedCardID) (*model.SavedCard, error)

	// FindAllByUserID 查询用户全部未删除的卡（Status != DELETED）
	FindAllByUserID(ctx context.Context, userID string) ([]*model.SavedCard, error)

	// FindDefaultByUserID 查询用户当前默认卡，无则返回 nil, nil
	FindDefaultByUserID(ctx context.Context, userID string) (*model.SavedCard, error)
}
