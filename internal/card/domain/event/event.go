// Package event 定义 card 上下文的领域事件。
// DomainEvent 接口统一由 internal/shared/event 提供，避免重复定义。
package event

import (
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
