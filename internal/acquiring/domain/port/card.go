package port

import "context"

// CardQuery acquiring 上下文对 card 上下文的 ACL 查询端口
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

// ResolvedCard card 服务解析后的权威卡展示信息 + 网关授权用 token（payment 视角 DTO）
type ResolvedCard struct {
	Last4        string
	Brand        string
	GatewayToken string // 含 RawPAN 的临时 vault token 或复用原 ct_*（若已是网关形态）
}

// CardCommand acquiring 上下文对 card 上下文的写入端口
type CardCommand interface {
	StoreChannelToken(ctx context.Context, cardID, channel, token, shopperRef string) error
	BindCardFromToken(ctx context.Context, req BindFromTokenCommand) (cardID string, err error)
	PrepareOneTimeToken(ctx context.Context, cardID, userID string) (cardToken string, err error)
	// ResolveCardForGateway 首购 ct_*：校验归属、解密 PAN、准备网关可用 token；Authorize 前调用（不消费原始 ct_*）
	ResolveCardForGateway(ctx context.Context, cardToken, userID string) (*ResolvedCard, error)
}

// BindFromTokenCommand 绑卡命令入参
type BindFromTokenCommand struct {
	CardToken  string
	Channel    string
	Token      string
	ShopperRef string
}
