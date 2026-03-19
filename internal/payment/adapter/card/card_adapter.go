package card

import (
	"context"

	cardModel "payment-demo/internal/card/domain/model"
	cardPort "payment-demo/internal/card/domain/port"
	"payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// CardAdapter ACL 适配器：card → payment 查询视图翻译
type CardAdapter struct {
	repo cardPort.CardRepository
}

var _ port.CardQuery = (*CardAdapter)(nil)

func NewCardAdapter(repo cardPort.CardRepository) *CardAdapter {
	return &CardAdapter{repo: repo}
}

func (a *CardAdapter) FindActiveCard(ctx context.Context, cardID string) (*port.SavedCardView, error) {
	card, err := a.repo.FindByID(ctx, cardModel.SavedCardID(cardID))
	if err != nil {
		return nil, model.ErrCardNotFound
	}

	tokens := make(map[string]string)
	for _, ct := range card.ChannelTokens {
		if ct.Status == cardModel.TokenStatusActive {
			tokens[ct.Channel] = ct.Token
		}
	}

	return &port.SavedCardView{
		CardID:        string(card.ID),
		UserID:        card.UserID,
		ChannelTokens: tokens,
		Last4:         card.Mask.Last4,
		Brand:         card.Mask.Brand,
		IsActive:      card.Status == cardModel.CardStatusActive,
	}, nil
}
