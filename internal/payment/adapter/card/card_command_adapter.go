package card

import (
	"context"

	cardApp "payment-demo/internal/card/application"
	cardModel "payment-demo/internal/card/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// CardCommandAdapter 实现 payment.port.CardCommand 接口
type CardCommandAdapter struct {
	cardUC *cardApp.CardUseCase
}

var _ port.CardCommand = (*CardCommandAdapter)(nil)

func NewCardCommandAdapter(cardUC *cardApp.CardUseCase) *CardCommandAdapter {
	return &CardCommandAdapter{cardUC: cardUC}
}

func (a *CardCommandAdapter) StoreChannelToken(ctx context.Context, cardID, channel, token, shopperRef string) error {
	return a.cardUC.StoreChannelToken(ctx, cardModel.SavedCardID(cardID), channel, token, shopperRef)
}

func (a *CardCommandAdapter) BindCardFromToken(ctx context.Context, req port.BindFromTokenCommand) (string, error) {
	card, err := a.cardUC.BindCardFromToken(ctx, cardApp.BindFromTokenRequest{
		CardToken:    req.CardToken,
		ChannelToken: req.Token,
		Channel:      req.Channel,
		ShopperRef:   req.ShopperRef,
	})
	if err != nil {
		return "", err
	}
	return string(card.ID), nil
}

func (a *CardCommandAdapter) PrepareOneTimeToken(ctx context.Context, cardID, userID string) (string, error) {
	return a.cardUC.PrepareOneTimeToken(ctx, cardModel.SavedCardID(cardID), userID)
}
