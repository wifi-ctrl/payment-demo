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
	CardStatusActive    CardStatus = "ACTIVE"
	CardStatusSuspended CardStatus = "SUSPENDED"
	CardStatusDeleted   CardStatus = "DELETED"
)

// CardMask 脱敏卡信息值对象（不可变，展示用）
type CardMask struct {
	Last4       string
	Brand       string
	ExpireMonth int
	ExpireYear  int
}

// CardHolder 持卡人值对象
type CardHolder struct {
	Name           string
	BillingCountry string
}

// SavedCard 已保存卡聚合根（Card Vault 模式）
// 状态机: Active ⇄ Suspended，Active/Suspended → Deleted（终态不可逆）
type SavedCard struct {
	ID            SavedCardID
	UserID        string
	EncryptedPAN  EncryptedPAN    // AES-256-GCM 加密的完整卡号
	PANHash       PANHash         // HMAC-SHA-256 哈希，查重用
	Mask          CardMask        // 脱敏展示信息
	Holder        CardHolder      // 持卡人
	ChannelTokens []ChannelToken  // 一卡多渠道的 recurring token
	IsDefault     bool
	Status        CardStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Events        []event.DomainEvent
}

// NewSavedCard 工厂方法
func NewSavedCard(
	userID string,
	encrypted EncryptedPAN,
	panHash PANHash,
	mask CardMask,
	holder CardHolder,
) *SavedCard {
	return &SavedCard{
		ID:           NewSavedCardID(),
		UserID:       userID,
		EncryptedPAN: encrypted,
		PANHash:      panHash,
		Mask:         mask,
		Holder:       holder,
		IsDefault:    false,
		Status:       CardStatusActive,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
}

// ── 绑卡行为 ──────────────────────────────────────────────────────

func (c *SavedCard) BindAsDefault() {
	c.IsDefault = true
	c.UpdatedAt = time.Now()
	c.emitCardBound(true)
}

func (c *SavedCard) Bind() {
	c.emitCardBound(false)
}

func (c *SavedCard) emitCardBound(isDefault bool) {
	c.addEvent(event.CardBound{
		CardID:     string(c.ID),
		UserID:     c.UserID,
		Last4:      c.Mask.Last4,
		Brand:      c.Mask.Brand,
		IsDefault:  isDefault,
		OccurredAt: time.Now(),
	})
}

// ── 状态转换 ──────────────────────────────────────────────────────

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

func (c *SavedCard) Delete() error {
	if c.Status == CardStatusDeleted {
		return ErrInvalidStateTransition
	}
	c.Status = CardStatusDeleted
	c.IsDefault = false
	c.UpdatedAt = time.Now()
	c.RevokeAllChannelTokens()
	c.addEvent(event.CardDeleted{
		CardID:     string(c.ID),
		UserID:     c.UserID,
		OccurredAt: time.Now(),
	})
	return nil
}

func (c *SavedCard) SetDefault() error {
	if c.Status != CardStatusActive {
		return ErrCardNotUsable
	}
	if c.IsDefault {
		return nil
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

func (c *SavedCard) UnsetDefault() {
	c.IsDefault = false
	c.UpdatedAt = time.Now()
}

// ── 加密相关行为 ──────────────────────────────────────────────────

// ReEncrypt 密钥轮换时用新密文替换旧密文
func (c *SavedCard) ReEncrypt(newEncrypted EncryptedPAN) {
	c.EncryptedPAN = newEncrypted
	c.UpdatedAt = time.Now()
}

// RecordPANDecryption 记录 PAN 解密审计事件（PCI Req 10）
func (c *SavedCard) RecordPANDecryption(reason string) {
	c.addEvent(event.PANDecrypted{
		CardID:     string(c.ID),
		UserID:     c.UserID,
		Reason:     reason,
		OccurredAt: time.Now(),
	})
}

// ── ChannelToken 行为 ─────────────────────────────────────────────

// StoreChannelToken 存储渠道复购令牌，一卡一渠道一令牌，相同渠道覆盖旧 token
func (c *SavedCard) StoreChannelToken(channel, token, shopperRef string) {
	for i, ct := range c.ChannelTokens {
		if ct.Channel == channel {
			c.ChannelTokens[i].Token = token
			c.ChannelTokens[i].ShopperRef = shopperRef
			c.ChannelTokens[i].Status = TokenStatusActive
			c.UpdatedAt = time.Now()
			c.addEvent(event.ChannelTokenStored{
				CardID:     string(c.ID),
				Channel:    channel,
				OccurredAt: time.Now(),
			})
			return
		}
	}
	c.ChannelTokens = append(c.ChannelTokens, ChannelToken{
		Channel:    channel,
		Token:      token,
		ShopperRef: shopperRef,
		Status:     TokenStatusActive,
		CreatedAt:  time.Now(),
	})
	c.UpdatedAt = time.Now()
	c.addEvent(event.ChannelTokenStored{
		CardID:     string(c.ID),
		Channel:    channel,
		OccurredAt: time.Now(),
	})
}

// GetActiveChannelToken 获取指定渠道的 active token
func (c *SavedCard) GetActiveChannelToken(channel string) *ChannelToken {
	for i, ct := range c.ChannelTokens {
		if ct.Channel == channel && ct.Status == TokenStatusActive {
			return &c.ChannelTokens[i]
		}
	}
	return nil
}

// RevokeChannelToken 吊销指定渠道的 token
func (c *SavedCard) RevokeChannelToken(channel string) {
	for i, ct := range c.ChannelTokens {
		if ct.Channel == channel && ct.Status == TokenStatusActive {
			c.ChannelTokens[i].Status = TokenStatusRevoked
			c.UpdatedAt = time.Now()
			c.addEvent(event.ChannelTokenRevoked{
				CardID:     string(c.ID),
				Channel:    channel,
				OccurredAt: time.Now(),
			})
			return
		}
	}
}

// RevokeAllChannelTokens 删卡时批量吊销所有 active token
func (c *SavedCard) RevokeAllChannelTokens() {
	for i, ct := range c.ChannelTokens {
		if ct.Status == TokenStatusActive {
			c.ChannelTokens[i].Status = TokenStatusRevoked
			c.addEvent(event.ChannelTokenRevoked{
				CardID:     string(c.ID),
				Channel:    ct.Channel,
				OccurredAt: time.Now(),
			})
		}
	}
}

// ── 事件基础设施 ──────────────────────────────────────────────────

func (c *SavedCard) addEvent(e event.DomainEvent) {
	c.Events = append(c.Events, e)
}

func (c *SavedCard) ClearEvents() []event.DomainEvent {
	events := c.Events
	c.Events = nil
	return events
}
