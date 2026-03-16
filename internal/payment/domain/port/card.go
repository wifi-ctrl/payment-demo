package port

import "context"

// CardQuery payment 上下文对 card 上下文的 ACL 查询端口
// 消费方（payment）定义，adapter 层（payment/adapter/card/）负责实现
// payment 上下文通过此端口获取已保存卡信息，不直接 import card 的 domain
type CardQuery interface {
	// FindActiveCard 按卡 ID 查询 Active 状态的卡，非 Active 返回错误
	FindActiveCard(ctx context.Context, cardID string) (*SavedCardView, error)
}

// SavedCardView payment 视角的卡视图（只含 payment 需要的字段）
// 与 card 上下文的 SavedCard 聚合根解耦
type SavedCardView struct {
	CardID   string
	UserID   string
	Token    string // VaultToken.Token，传递给 PaymentGateway
	Last4    string
	Brand    string
	IsActive bool
}
