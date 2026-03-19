package model

import "time"

// TokenStatus 渠道 Token 状态
type TokenStatus string

const (
	TokenStatusActive  TokenStatus = "active"
	TokenStatusRevoked TokenStatus = "revoked"
)

// ChannelToken 渠道复购令牌（Card 聚合的子实体）
type ChannelToken struct {
	Channel    string
	Token      string
	ShopperRef string
	Status     TokenStatus
	CreatedAt  time.Time
}
