package port

import (
	"context"

	"payment-demo/internal/card/domain/model"
)

// CardRepository 卡仓储端口（被驱动端口）
type CardRepository interface {
	Save(ctx context.Context, card *model.SavedCard) error
	FindByID(ctx context.Context, id model.SavedCardID) (*model.SavedCard, error)
	FindAllByUserID(ctx context.Context, userID string) ([]*model.SavedCard, error)
	FindDefaultByUserID(ctx context.Context, userID string) (*model.SavedCard, error)
	FindActiveByUserAndPANHash(ctx context.Context, userID string, panHash model.PANHash) (*model.SavedCard, error)
	FindByKeyVersion(ctx context.Context, keyVersion int) ([]*model.SavedCard, error)
}
