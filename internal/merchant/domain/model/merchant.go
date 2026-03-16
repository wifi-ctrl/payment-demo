// Package model 定义 merchant 上下文的聚合根、实体与值对象。
package model

import (
	"time"

	"github.com/google/uuid"

	"payment-demo/internal/merchant/domain/event"
)

// ── 标识符 ────────────────────────────────────────────────────────

// MerchantID 商户全局唯一标识（值对象）。
type MerchantID string

// NewMerchantID 生成新的商户 ID。
func NewMerchantID() MerchantID { return MerchantID(uuid.New().String()) }

// ── 商户状态枚举 ──────────────────────────────────────────────────

// MerchantStatus 商户的激活状态（值对象枚举）。
type MerchantStatus string

const (
	MerchantStatusActive      MerchantStatus = "ACTIVE"
	MerchantStatusSuspended   MerchantStatus = "SUSPENDED"
	MerchantStatusDeactivated MerchantStatus = "DEACTIVATED"
)

// ── 渠道枚举（对齐 payment.PaymentMethod） ────────────────────────

// PaymentChannel 支付渠道枚举，与 payment 上下文的 PaymentMethod 保持字面值一致。
type PaymentChannel string

const (
	ChannelCard   PaymentChannel = "CARD"
	ChannelPayPal PaymentChannel = "PAYPAL"
)

// ── ChannelCredential 实体 ────────────────────────────────────────

// ChannelCredentialID 渠道凭据唯一标识（值对象）。
type ChannelCredentialID string

// NewChannelCredentialID 生成新的凭据 ID。
func NewChannelCredentialID() ChannelCredentialID {
	return ChannelCredentialID(uuid.New().String())
}

// CredentialStatus 凭据启用状态（值对象枚举）。
type CredentialStatus string

const (
	CredentialStatusActive  CredentialStatus = "ACTIVE"
	CredentialStatusRevoked CredentialStatus = "REVOKED"
)

// ChannelCredential 商户在某支付渠道的接入凭据（实体）。
// Secrets 以 map 形式存储渠道密钥，key 为语义字段名（如 "api_key"、"client_id"），
// 不在聚合根层做序列化或加密，Infra 层负责安全存储。
type ChannelCredential struct {
	ID         ChannelCredentialID
	MerchantID MerchantID
	Channel    PaymentChannel
	Secrets    map[string]string // e.g. {"api_key":"sk_live_xxx"} / {"client_id":"...","client_secret":"..."}
	Status     CredentialStatus
	CreatedAt  time.Time
	RevokedAt  *time.Time
}

// ── Merchant 聚合根 ───────────────────────────────────────────────

// Merchant 商户聚合根。
// 不变式：同一渠道最多只有一个 ACTIVE 凭据。
type Merchant struct {
	ID          MerchantID
	Name        string
	Status      MerchantStatus
	Credentials []ChannelCredential
	CreatedAt   time.Time
	Events      []event.DomainEvent // 未发布的领域事件，由 UseCase 调用 ClearEvents 后发布
}

// NewMerchant 工厂方法：创建新商户，初始状态 ACTIVE，触发 MerchantRegistered 事件。
func NewMerchant(name string) *Merchant {
	m := &Merchant{
		ID:        NewMerchantID(),
		Name:      name,
		Status:    MerchantStatusActive,
		CreatedAt: time.Now(),
	}
	m.addEvent(event.MerchantRegistered{
		MerchantID: string(m.ID),
		Name:       name,
		OccurredAt: m.CreatedAt,
	})
	return m
}

// AddCredential 为商户添加渠道凭据。
// 不变式：若该渠道已有 ACTIVE 凭据，须先调用 RevokeCredential 吊销旧凭据；
// 否则返回 ErrCredentialAlreadyExists，防止同一渠道并发激活。
func (m *Merchant) AddCredential(channel PaymentChannel, secrets map[string]string) error {
	if m.Status != MerchantStatusActive {
		return ErrMerchantNotActive
	}
	for _, c := range m.Credentials {
		if c.Channel == channel && c.Status == CredentialStatusActive {
			return ErrCredentialAlreadyExists
		}
	}
	cred := ChannelCredential{
		ID:         NewChannelCredentialID(),
		MerchantID: m.ID,
		Channel:    channel,
		Secrets:    secrets,
		Status:     CredentialStatusActive,
		CreatedAt:  time.Now(),
	}
	m.Credentials = append(m.Credentials, cred)
	m.addEvent(event.CredentialAdded{
		MerchantID:   string(m.ID),
		CredentialID: string(cred.ID),
		Channel:      string(channel),
		OccurredAt:   cred.CreatedAt,
	})
	return nil
}

// RevokeCredential 吊销指定 ID 的渠道凭据：ACTIVE → REVOKED。
// 非法状态转换（已 REVOKED）返回 ErrInvalidStateTransition。
// 凭据不存在返回 ErrCredentialNotFound。
func (m *Merchant) RevokeCredential(credentialID ChannelCredentialID) error {
	for i, c := range m.Credentials {
		if c.ID == credentialID {
			if c.Status != CredentialStatusActive {
				return ErrInvalidStateTransition
			}
			now := time.Now()
			m.Credentials[i].Status = CredentialStatusRevoked
			m.Credentials[i].RevokedAt = &now
			m.addEvent(event.CredentialRevoked{
				MerchantID:   string(m.ID),
				CredentialID: string(credentialID),
				Channel:      string(c.Channel),
				OccurredAt:   now,
			})
			return nil
		}
	}
	return ErrCredentialNotFound
}

// Suspend 暂停商户：ACTIVE → SUSPENDED。
// 非 ACTIVE 状态调用返回 ErrInvalidStateTransition。
func (m *Merchant) Suspend() error {
	if m.Status != MerchantStatusActive {
		return ErrInvalidStateTransition
	}
	m.Status = MerchantStatusSuspended
	m.addEvent(event.MerchantSuspended{
		MerchantID: string(m.ID),
		OccurredAt: time.Now(),
	})
	return nil
}

// ActiveCredential 查询指定渠道的当前有效凭据（只读辅助方法）。
// 无 ACTIVE 凭据时返回 ErrCredentialNotFound。
func (m *Merchant) ActiveCredential(channel PaymentChannel) (*ChannelCredential, error) {
	for i, c := range m.Credentials {
		if c.Channel == channel && c.Status == CredentialStatusActive {
			return &m.Credentials[i], nil
		}
	}
	return nil, ErrCredentialNotFound
}

func (m *Merchant) addEvent(e event.DomainEvent) {
	m.Events = append(m.Events, e)
}

// ClearEvents 返回所有未发布的领域事件并清空，由 UseCase 调用后统一发布。
func (m *Merchant) ClearEvents() []event.DomainEvent {
	events := m.Events
	m.Events = nil
	return events
}
