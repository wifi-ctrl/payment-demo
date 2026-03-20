package event

import (
	"time"

	sharedEvent "payment-demo/internal/shared/event"
)

// DomainEvent 是 shared/event.DomainEvent 的本包别名，供上层无缝使用。
type DomainEvent = sharedEvent.DomainEvent

// MerchantRegistered 商户完成注册后触发。
type MerchantRegistered struct {
	MerchantID string
	Name       string
	OccurredAt time.Time
}

func (e MerchantRegistered) EventName() string { return "merchant.registered" }

// CredentialAdded 商户为某渠道添加凭据后触发。
type CredentialAdded struct {
	MerchantID   string
	CredentialID string
	Channel      string // "CARD" / "PAYPAL"
	OccurredAt   time.Time
}

func (e CredentialAdded) EventName() string { return "merchant.credential_added" }

// CredentialRevoked 渠道凭据被吊销后触发。
type CredentialRevoked struct {
	MerchantID   string
	CredentialID string
	Channel      string
	OccurredAt   time.Time
}

func (e CredentialRevoked) EventName() string { return "merchant.credential_revoked" }

// MerchantSuspended 商户被暂停时触发。
type MerchantSuspended struct {
	MerchantID string
	OccurredAt time.Time
}

func (e MerchantSuspended) EventName() string { return "merchant.suspended" }
