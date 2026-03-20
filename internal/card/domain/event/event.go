// Package event 定义 card 上下文的领域事件。
// DomainEvent 接口统一由 internal/shared/event 提供，避免重复定义。
package event

import (
	"fmt"
	"time"

	sharedEvent "payment-demo/internal/shared/event"
)

// DomainEvent 是 shared/event.DomainEvent 的本包别名，供上层无缝使用。
type DomainEvent = sharedEvent.DomainEvent

// CardBound 卡已绑定
type CardBound struct {
	CardID     string
	UserID     string
	Last4      string
	Brand      string
	IsDefault  bool
	OccurredAt time.Time
}

func (e CardBound) EventName() string { return "card.bound" }

// CardSuspended 卡已挂起
type CardSuspended struct {
	CardID     string
	UserID     string
	OccurredAt time.Time
}

func (e CardSuspended) EventName() string { return "card.suspended" }

// CardActivated 卡已激活（Suspended → Active）
type CardActivated struct {
	CardID     string
	UserID     string
	OccurredAt time.Time
}

func (e CardActivated) EventName() string { return "card.activated" }

// CardDeleted 卡已删除
type CardDeleted struct {
	CardID     string
	UserID     string
	OccurredAt time.Time
}

func (e CardDeleted) EventName() string { return "card.deleted" }

// DefaultCardChanged 默认卡已变更
type DefaultCardChanged struct {
	CardID     string
	UserID     string
	OccurredAt time.Time
}

func (e DefaultCardChanged) EventName() string { return "card.default_changed" }

// ChannelTokenStored 渠道复购令牌已存储
type ChannelTokenStored struct {
	CardID     string
	Channel    string
	OccurredAt time.Time
}

func (e ChannelTokenStored) EventName() string { return "card.channel_token_stored" }

// ChannelTokenRevoked 渠道复购令牌已吊销
type ChannelTokenRevoked struct {
	CardID     string
	Channel    string
	OccurredAt time.Time
}

func (e ChannelTokenRevoked) EventName() string { return "card.channel_token_revoked" }

// PANDecrypted PAN 已解密（PCI Req 10 审计事件）
type PANDecrypted struct {
	CardID     string
	UserID     string
	Reason     string
	OccurredAt time.Time
}

func (e PANDecrypted) EventName() string { return "card.pan_decrypted" }

// String PCI Req 10: 审计日志安全格式，防止 %+v 泄漏未来可能新增的敏感字段
func (e PANDecrypted) String() string {
	return fmt.Sprintf("{CardID:%s UserID:%s Reason:%s At:%s}", e.CardID, e.UserID, e.Reason, e.OccurredAt.Format(time.RFC3339))
}
