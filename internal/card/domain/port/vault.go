package port

import (
	"context"

	"payment-demo/internal/card/domain/model"
)

// TokenizeResult 令牌化结果
type TokenizeResult struct {
	CardToken      *string           // 临时令牌 "ct_" + UUID（查重命中时为 nil）
	Mask           model.CardMask
	Brand          model.CardBrand
	ExistingCardID *model.SavedCardID // 查重命中时返回已有 card_id
}

// CachedCardData 从临时缓存中取出的卡数据（一次性消费）
type CachedCardData struct {
	EncryptedPAN model.EncryptedPAN // 首次令牌化时存密文（绑卡持久化用）
	RawPAN       string             // PrepareOneTimeToken 时存明文（Gateway 直接用）
	PANHash      model.PANHash
	Mask         model.CardMask
	Holder       model.CardHolder
	UserID       string
}

// CardVault 卡数据保险库端口（被驱动端口）
type CardVault interface {
	// CacheTokenizedCard 将加密后的卡数据临时缓存，返回 card_token
	CacheTokenizedCard(ctx context.Context, data CachedCardData) (cardToken string, err error)

	// PeekCachedCard 校验临时 token 存在、未过期且归属指定用户，并返回缓存数据（不消费 token）
	PeekCachedCard(ctx context.Context, cardToken, userID string) (*CachedCardData, error)

	// ConsumeCardToken 原子取出并删除临时卡数据（GETDEL 语义）
	ConsumeCardToken(ctx context.Context, cardToken string) (*CachedCardData, error)
}
