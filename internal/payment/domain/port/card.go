package port

import "context"

// CardQuery payment 上下文对 card 上下文的 ACL 查询端口
type CardQuery interface {
	FindActiveCard(ctx context.Context, cardID string) (*SavedCardView, error)
}

// SavedCardView payment 视角的卡视图
type SavedCardView struct {
	CardID        string
	UserID        string
	ChannelTokens map[string]string // channel → recurring token
	Last4         string
	Brand         string
	IsActive      bool
}

// CardCommand payment 上下文对 card 上下文的写入端口
type CardCommand interface {
	StoreChannelToken(ctx context.Context, cardID, channel, token, shopperRef string) error
	BindCardFromToken(ctx context.Context, req BindFromTokenCommand) (cardID string, err error)
	PrepareOneTimeToken(ctx context.Context, cardID, userID string) (cardToken string, err error)
}

// BindFromTokenCommand 绑卡命令入参
type BindFromTokenCommand struct {
	CardToken  string
	Channel    string
	Token      string
	ShopperRef string
}
