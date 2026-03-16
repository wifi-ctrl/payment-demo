package model

import (
	"time"

	"github.com/google/uuid"

	"payment-demo/internal/card/domain/event"
)

// SavedCardID 已保存卡的唯一标识
type SavedCardID string

func NewSavedCardID() SavedCardID {
	return SavedCardID(uuid.New().String())
}

// CardStatus 卡状态枚举
type CardStatus string

const (
	CardStatusActive    CardStatus = "ACTIVE"    // 正常可用
	CardStatusSuspended CardStatus = "SUSPENDED" // 已挂起（冻结）
	CardStatusDeleted   CardStatus = "DELETED"   // 已删除（软删）
)

// VaultToken 第三方 Vault 持久令牌（不透明值对象）
type VaultToken struct {
	Token    string // Vault 侧唯一标识
	Provider string // 如 "stripe"、"braintree"
}

// CardMask 脱敏卡信息值对象（不可变，展示用）
type CardMask struct {
	Last4       string // 后四位
	Brand       string // Visa / Mastercard / UnionPay ...
	ExpireMonth int    // 1-12
	ExpireYear  int    // 如 2028
}

// CardHolder 持卡人值对象
type CardHolder struct {
	Name           string
	BillingCountry string // ISO 3166-1 alpha-2，如 "US"
}

// SavedCard 已保存卡聚合根
// 状态机: Active ⇄ Suspended，Active/Suspended → Deleted（终态不可逆）
type SavedCard struct {
	ID         SavedCardID
	UserID     string     // 归属用户，来自 identity 上下文 ctx
	VaultToken VaultToken // 托管在 Vault 的持久令牌，替代原始卡号
	Mask       CardMask   // 脱敏展示信息
	Holder     CardHolder // 持卡人
	IsDefault  bool       // 是否为用户默认支付卡
	Status     CardStatus
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Events     []event.DomainEvent // 未发布的领域事件
}

// NewSavedCard 工厂方法：创建一张新绑定的卡（初始 Active）
// vaultToken 由 application 层调用 CardVault Gateway 换取后传入
func NewSavedCard(userID string, token VaultToken, mask CardMask, holder CardHolder) *SavedCard {
	return &SavedCard{
		ID:         NewSavedCardID(),
		UserID:     userID,
		VaultToken: token,
		Mask:       mask,
		Holder:     holder,
		IsDefault:  false,
		Status:     CardStatusActive,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

// BindAsDefault 绑卡后标记为默认卡，同时发布 CardBound 事件
// 由 usecase 判断"用户尚无已有卡"时调用；若已有卡则只调 Bind（不设默认）
func (c *SavedCard) BindAsDefault() {
	c.IsDefault = true
	c.UpdatedAt = time.Now()
	c.addEvent(event.CardBound{
		CardID:     string(c.ID),
		UserID:     c.UserID,
		Last4:      c.Mask.Last4,
		Brand:      c.Mask.Brand,
		IsDefault:  c.IsDefault,
		OccurredAt: time.Now(),
	})
}

// Bind 绑卡（非默认），仅发布 CardBound 事件
func (c *SavedCard) Bind() {
	c.addEvent(event.CardBound{
		CardID:     string(c.ID),
		UserID:     c.UserID,
		Last4:      c.Mask.Last4,
		Brand:      c.Mask.Brand,
		IsDefault:  false,
		OccurredAt: time.Now(),
	})
}

// Suspend 挂起卡（Active → Suspended）
func (c *SavedCard) Suspend() error {
	if c.Status != CardStatusActive {
		return ErrInvalidStateTransition
	}
	c.Status = CardStatusSuspended
	c.UpdatedAt = time.Now()
	c.addEvent(event.CardSuspended{
		CardID:     string(c.ID),
		UserID:     c.UserID,
		OccurredAt: time.Now(),
	})
	return nil
}

// Activate 解除挂起（Suspended → Active）
func (c *SavedCard) Activate() error {
	if c.Status != CardStatusSuspended {
		return ErrInvalidStateTransition
	}
	c.Status = CardStatusActive
	c.UpdatedAt = time.Now()
	c.addEvent(event.CardActivated{
		CardID:     string(c.ID),
		UserID:     c.UserID,
		OccurredAt: time.Now(),
	})
	return nil
}

// Delete 软删除（Active/Suspended → Deleted，终态不可逆）
func (c *SavedCard) Delete() error {
	if c.Status == CardStatusDeleted {
		return ErrInvalidStateTransition
	}
	c.Status = CardStatusDeleted
	c.IsDefault = false
	c.UpdatedAt = time.Now()
	c.addEvent(event.CardDeleted{
		CardID:     string(c.ID),
		UserID:     c.UserID,
		OccurredAt: time.Now(),
	})
	return nil
}

// SetDefault 设为默认卡（仅 Active 状态可设）
// 注意：usecase 负责先调用旧默认卡的 UnsetDefault，再调用本方法
func (c *SavedCard) SetDefault() error {
	if c.Status != CardStatusActive {
		return ErrCardNotUsable
	}
	c.IsDefault = true
	c.UpdatedAt = time.Now()
	c.addEvent(event.DefaultCardChanged{
		CardID:     string(c.ID),
		UserID:     c.UserID,
		OccurredAt: time.Now(),
	})
	return nil
}

// UnsetDefault 取消默认（由 usecase 在切换默认卡前调用旧卡）
func (c *SavedCard) UnsetDefault() {
	c.IsDefault = false
	c.UpdatedAt = time.Now()
}

func (c *SavedCard) addEvent(e event.DomainEvent) {
	c.Events = append(c.Events, e)
}

// ClearEvents 返回并清空待发布事件
func (c *SavedCard) ClearEvents() []event.DomainEvent {
	events := c.Events
	c.Events = nil
	return events
}
