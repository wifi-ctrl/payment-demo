package card

import (
	"context"

	cardModel "payment-demo/internal/card/domain/model"
	cardPort "payment-demo/internal/card/domain/port"
	"payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// CardAdapter ACL 适配器
// 实现 payment 的 CardQuery 端口，内部调用 card 上下文的 CardRepository
// 翻译：card.SavedCard → payment.SavedCardView
type CardAdapter struct {
	repo cardPort.CardRepository
}

// 编译期检查接口实现
var _ port.CardQuery = (*CardAdapter)(nil)

func NewCardAdapter(repo cardPort.CardRepository) *CardAdapter {
	return &CardAdapter{repo: repo}
}

// FindActiveCard 按卡 ID 查询 Active 状态的卡，非 Active 返回错误
func (a *CardAdapter) FindActiveCard(ctx context.Context, cardID string) (*port.SavedCardView, error) {
	// 调用 card 上下文的仓储
	card, err := a.repo.FindByID(ctx, cardModel.SavedCardID(cardID))
	if err != nil {
		return nil, model.ErrCardNotFound
	}

	// ACL 翻译：card 的聚合根 → payment 的极简视图
	return &port.SavedCardView{
		CardID:   string(card.ID),
		UserID:   card.UserID,
		Token:    card.VaultToken.Token,
		Last4:    card.Mask.Last4,
		Brand:    card.Mask.Brand,
		IsActive: card.Status == cardModel.CardStatusActive,
	}, nil
}
