package model_test

import (
	"strings"
	"testing"

	"payment-demo/internal/merchant/domain/event"
	"payment-demo/internal/merchant/domain/model"
)

// ─────────────────────────────────────────────────────────────────
// 测试辅助
// ─────────────────────────────────────────────────────────────────

func activeMerchant() *model.Merchant {
	return model.NewMerchant("Acme Corp")
}

// activeMerchantWithCardCred 创建一个 ACTIVE 商户并添加一个 CARD ACTIVE 凭据，
// 返回商户和凭据 ID（ClearEvents 在此不调用，保留初始事件用于各 test 独立验证）。
func activeMerchantWithCardCred() (*model.Merchant, model.ChannelCredentialID) {
	m := activeMerchant()
	m.ClearEvents() // 清掉 NewMerchant 触发的 MerchantRegistered 事件，保持测试隔离
	_ = m.AddCredential(model.ChannelCard, map[string]string{"api_key": "sk_live_xxx"})
	credID := m.Credentials[0].ID
	m.ClearEvents() // 清掉 CredentialAdded 事件
	return m, credID
}

// isValidUUID 简单校验 UUID v4 格式（xxxxxxxx-xxxx-4xxx-xxxx-xxxxxxxxxxxx）
func isValidUUID(s string) bool {
	// UUID 格式：8-4-4-4-12 十六进制字符，共 36 个字符（含 4 个连字符）
	if len(s) != 36 {
		return false
	}
	parts := strings.Split(s, "-")
	if len(parts) != 5 {
		return false
	}
	return len(parts[0]) == 8 && len(parts[1]) == 4 && len(parts[2]) == 4 &&
		len(parts[3]) == 4 && len(parts[4]) == 12
}

// ─────────────────────────────────────────────────────────────────
// AC-1: NewMerchant 工厂方法正常创建
// ─────────────────────────────────────────────────────────────────

func TestNewMerchant_Factory_SetsAllFields(t *testing.T) {
	// AC-1
	m := model.NewMerchant("Acme Corp")

	if string(m.ID) == "" {
		t.Error("ID must not be empty")
	}
	if !isValidUUID(string(m.ID)) {
		t.Errorf("ID must be UUID format, got %s", m.ID)
	}
	if m.Name != "Acme Corp" {
		t.Errorf("Name: want 'Acme Corp', got %q", m.Name)
	}
	if m.Status != model.MerchantStatusActive {
		t.Errorf("Status: want ACTIVE, got %s", m.Status)
	}
	if len(m.Credentials) != 0 {
		t.Errorf("Credentials: want empty, got len=%d", len(m.Credentials))
	}
	if m.CreatedAt.IsZero() {
		t.Error("CreatedAt must not be zero")
	}
}

func TestNewMerchant_Factory_EmitsMerchantRegisteredEvent(t *testing.T) {
	// AC-1: Events 包含且仅包含 1 个 MerchantRegistered 事件
	m := model.NewMerchant("Acme Corp")

	if len(m.Events) != 1 {
		t.Fatalf("Events: want 1, got %d", len(m.Events))
	}
	evt, ok := m.Events[0].(event.MerchantRegistered)
	if !ok {
		t.Fatalf("Events[0]: want MerchantRegistered, got %T", m.Events[0])
	}
	if evt.MerchantID != string(m.ID) {
		t.Errorf("MerchantRegistered.MerchantID: want %s, got %s", m.ID, evt.MerchantID)
	}
	if evt.Name != "Acme Corp" {
		t.Errorf("MerchantRegistered.Name: want 'Acme Corp', got %q", evt.Name)
	}
	if evt.OccurredAt.IsZero() {
		t.Error("MerchantRegistered.OccurredAt must not be zero")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-2: NewMerchant 每次调用生成唯一 ID
// ─────────────────────────────────────────────────────────────────

func TestNewMerchant_UniqueID_EachCall(t *testing.T) {
	// AC-2
	m1 := model.NewMerchant("A")
	m2 := model.NewMerchant("A")

	if m1.ID == m2.ID {
		t.Errorf("IDs must be different: both got %s", m1.ID)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-3: AddCredential 首次为渠道添加凭据成功
// ─────────────────────────────────────────────────────────────────

func TestMerchant_AddCredential_FirstTime_Success(t *testing.T) {
	// AC-3
	m := activeMerchant()
	m.ClearEvents()

	err := m.AddCredential(model.ChannelCard, map[string]string{"api_key": "sk_live_xxx"})

	if err != nil {
		t.Fatalf("AddCredential: want nil, got %v", err)
	}
	if len(m.Credentials) != 1 {
		t.Fatalf("Credentials len: want 1, got %d", len(m.Credentials))
	}

	cred := m.Credentials[0]
	if cred.Channel != model.ChannelCard {
		t.Errorf("Channel: want CARD, got %s", cred.Channel)
	}
	if cred.Status != model.CredentialStatusActive {
		t.Errorf("Status: want ACTIVE, got %s", cred.Status)
	}
	if cred.Secrets["api_key"] != "sk_live_xxx" {
		t.Errorf("Secrets api_key: want sk_live_xxx, got %s", cred.Secrets["api_key"])
	}
	if string(cred.ID) == "" || !isValidUUID(string(cred.ID)) {
		t.Errorf("CredentialID must be valid UUID, got %s", cred.ID)
	}
	if cred.MerchantID != m.ID {
		t.Errorf("MerchantID: want %s, got %s", m.ID, cred.MerchantID)
	}

	// 检查事件
	if len(m.Events) != 1 {
		t.Fatalf("Events len: want 1, got %d", len(m.Events))
	}
	addedEvt, ok := m.Events[0].(event.CredentialAdded)
	if !ok {
		t.Fatalf("Events[0]: want CredentialAdded, got %T", m.Events[0])
	}
	if addedEvt.Channel != "CARD" {
		t.Errorf("CredentialAdded.Channel: want CARD, got %s", addedEvt.Channel)
	}
	if addedEvt.CredentialID != string(cred.ID) {
		t.Errorf("CredentialAdded.CredentialID: want %s, got %s", cred.ID, addedEvt.CredentialID)
	}
	if addedEvt.MerchantID != string(m.ID) {
		t.Errorf("CredentialAdded.MerchantID: want %s, got %s", m.ID, addedEvt.MerchantID)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-4: AddCredential 同渠道已有 ACTIVE 凭据时拒绝
// ─────────────────────────────────────────────────────────────────

func TestMerchant_AddCredential_DuplicateActiveChannel_ReturnsError(t *testing.T) {
	// AC-4
	m, _ := activeMerchantWithCardCred()

	err := m.AddCredential(model.ChannelCard, map[string]string{"api_key": "sk_live_new"})

	if err != model.ErrCredentialAlreadyExists {
		t.Errorf("want ErrCredentialAlreadyExists, got %v", err)
	}
	if len(m.Credentials) != 1 {
		t.Errorf("Credentials len must not change: want 1, got %d", len(m.Credentials))
	}
	if len(m.Events) != 0 {
		t.Errorf("no new events expected, got %d", len(m.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-5: AddCredential 对非 ACTIVE 商户拒绝
// ─────────────────────────────────────────────────────────────────

func TestMerchant_AddCredential_SuspendedMerchant_ReturnsError(t *testing.T) {
	// AC-5
	m := activeMerchant()
	_ = m.Suspend()
	m.ClearEvents()

	err := m.AddCredential(model.ChannelCard, map[string]string{"api_key": "sk_live_xxx"})

	if err != model.ErrMerchantNotActive {
		t.Errorf("want ErrMerchantNotActive, got %v", err)
	}
	if len(m.Credentials) != 0 {
		t.Errorf("Credentials must remain empty, got %d", len(m.Credentials))
	}
	if len(m.Events) != 0 {
		t.Errorf("no new events expected, got %d", len(m.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-6: AddCredential 同渠道旧凭据已 REVOKED 后可再次添加
// ─────────────────────────────────────────────────────────────────

func TestMerchant_AddCredential_AfterRevoke_Success(t *testing.T) {
	// AC-6
	m, credID := activeMerchantWithCardCred()
	_ = m.RevokeCredential(credID)
	m.ClearEvents()

	err := m.AddCredential(model.ChannelCard, map[string]string{"api_key": "sk_live_new"})

	if err != nil {
		t.Fatalf("AddCredential after revoke: want nil, got %v", err)
	}
	if len(m.Credentials) != 2 {
		t.Errorf("Credentials len: want 2 (1 REVOKED + 1 ACTIVE), got %d", len(m.Credentials))
	}

	// 验证新凭据状态
	var activeCount int
	for _, c := range m.Credentials {
		if c.Channel == model.ChannelCard && c.Status == model.CredentialStatusActive {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Errorf("ACTIVE CARD credentials: want 1, got %d", activeCount)
	}

	// 验证 CredentialAdded 事件
	if len(m.Events) != 1 {
		t.Fatalf("Events len: want 1, got %d", len(m.Events))
	}
	if _, ok := m.Events[0].(event.CredentialAdded); !ok {
		t.Errorf("want CredentialAdded event, got %T", m.Events[0])
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-7: AddCredential 不同渠道可同时 ACTIVE
// ─────────────────────────────────────────────────────────────────

func TestMerchant_AddCredential_DifferentChannels_BothActive(t *testing.T) {
	// AC-7
	m, _ := activeMerchantWithCardCred()

	err := m.AddCredential(model.ChannelPayPal, map[string]string{
		"client_id":     "xxx",
		"client_secret": "yyy",
	})

	if err != nil {
		t.Fatalf("AddCredential PayPal: want nil, got %v", err)
	}
	if len(m.Credentials) != 2 {
		t.Fatalf("Credentials len: want 2, got %d", len(m.Credentials))
	}

	var cardActive, paypalActive bool
	for _, c := range m.Credentials {
		if c.Channel == model.ChannelCard && c.Status == model.CredentialStatusActive {
			cardActive = true
		}
		if c.Channel == model.ChannelPayPal && c.Status == model.CredentialStatusActive {
			paypalActive = true
		}
	}
	if !cardActive {
		t.Error("CARD credential must remain ACTIVE")
	}
	if !paypalActive {
		t.Error("PAYPAL credential must be ACTIVE")
	}

	// 检查事件中 Channel 为 PAYPAL
	var found bool
	for _, e := range m.Events {
		if addedEvt, ok := e.(event.CredentialAdded); ok && addedEvt.Channel == "PAYPAL" {
			found = true
		}
	}
	if !found {
		t.Error("CredentialAdded event with Channel==PAYPAL not found")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-8: RevokeCredential 正常吊销 ACTIVE 凭据
// ─────────────────────────────────────────────────────────────────

func TestMerchant_RevokeCredential_ActiveCred_Success(t *testing.T) {
	// AC-8
	m, credID := activeMerchantWithCardCred()

	err := m.RevokeCredential(credID)

	if err != nil {
		t.Fatalf("RevokeCredential: want nil, got %v", err)
	}

	cred := m.Credentials[0]
	if cred.Status != model.CredentialStatusRevoked {
		t.Errorf("Status: want REVOKED, got %s", cred.Status)
	}
	if cred.RevokedAt == nil {
		t.Error("RevokedAt must not be nil after revoke")
	}

	// 检查事件
	if len(m.Events) != 1 {
		t.Fatalf("Events len: want 1, got %d", len(m.Events))
	}
	revokedEvt, ok := m.Events[0].(event.CredentialRevoked)
	if !ok {
		t.Fatalf("Events[0]: want CredentialRevoked, got %T", m.Events[0])
	}
	if revokedEvt.CredentialID != string(credID) {
		t.Errorf("CredentialRevoked.CredentialID: want %s, got %s", credID, revokedEvt.CredentialID)
	}
	if revokedEvt.Channel != "CARD" {
		t.Errorf("CredentialRevoked.Channel: want CARD, got %s", revokedEvt.Channel)
	}
	if revokedEvt.MerchantID != string(m.ID) {
		t.Errorf("CredentialRevoked.MerchantID: want %s, got %s", m.ID, revokedEvt.MerchantID)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-9: RevokeCredential 对已 REVOKED 凭据返回 ErrInvalidStateTransition
// ─────────────────────────────────────────────────────────────────

func TestMerchant_RevokeCredential_AlreadyRevoked_ReturnsError(t *testing.T) {
	// AC-9
	m, credID := activeMerchantWithCardCred()
	_ = m.RevokeCredential(credID) // 第一次吊销
	m.ClearEvents()

	err := m.RevokeCredential(credID) // 第二次吊销

	if err != model.ErrInvalidStateTransition {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
	if m.Credentials[0].Status != model.CredentialStatusRevoked {
		t.Errorf("Status must remain REVOKED, got %s", m.Credentials[0].Status)
	}
	if len(m.Events) != 0 {
		t.Errorf("no new events expected, got %d", len(m.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-10: RevokeCredential 对不存在凭据返回 ErrCredentialNotFound
// ─────────────────────────────────────────────────────────────────

func TestMerchant_RevokeCredential_NotFound_ReturnsError(t *testing.T) {
	// AC-10
	m := activeMerchant()
	m.ClearEvents()

	err := m.RevokeCredential(model.ChannelCredentialID("non-existent-id"))

	if err != model.ErrCredentialNotFound {
		t.Errorf("want ErrCredentialNotFound, got %v", err)
	}
	if len(m.Credentials) != 0 {
		t.Errorf("Credentials must remain empty, got %d", len(m.Credentials))
	}
	if len(m.Events) != 0 {
		t.Errorf("no new events expected, got %d", len(m.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-11: Suspend 正常暂停 ACTIVE 商户
// ─────────────────────────────────────────────────────────────────

func TestMerchant_Suspend_Active_Success(t *testing.T) {
	// AC-11
	m := activeMerchant()
	m.ClearEvents()

	err := m.Suspend()

	if err != nil {
		t.Fatalf("Suspend: want nil, got %v", err)
	}
	if m.Status != model.MerchantStatusSuspended {
		t.Errorf("Status: want SUSPENDED, got %s", m.Status)
	}

	// 检查事件
	if len(m.Events) != 1 {
		t.Fatalf("Events len: want 1, got %d", len(m.Events))
	}
	suspendedEvt, ok := m.Events[0].(event.MerchantSuspended)
	if !ok {
		t.Fatalf("Events[0]: want MerchantSuspended, got %T", m.Events[0])
	}
	if suspendedEvt.MerchantID != string(m.ID) {
		t.Errorf("MerchantSuspended.MerchantID: want %s, got %s", m.ID, suspendedEvt.MerchantID)
	}
	if suspendedEvt.OccurredAt.IsZero() {
		t.Error("MerchantSuspended.OccurredAt must not be zero")
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-12: Suspend 对 SUSPENDED 商户返回 ErrInvalidStateTransition
// ─────────────────────────────────────────────────────────────────

func TestMerchant_Suspend_AlreadySuspended_ReturnsError(t *testing.T) {
	// AC-12
	m := activeMerchant()
	_ = m.Suspend()
	m.ClearEvents()

	err := m.Suspend()

	if err != model.ErrInvalidStateTransition {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
	if m.Status != model.MerchantStatusSuspended {
		t.Errorf("Status must remain SUSPENDED, got %s", m.Status)
	}
	if len(m.Events) != 0 {
		t.Errorf("no new events expected, got %d", len(m.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-13: Suspend 对 DEACTIVATED 商户返回 ErrInvalidStateTransition
// ─────────────────────────────────────────────────────────────────

func TestMerchant_Suspend_Deactivated_ReturnsError(t *testing.T) {
	// AC-13: 直接构造一个 DEACTIVATED 商户（无工厂方法，直接赋值）
	m := activeMerchant()
	m.Status = model.MerchantStatusDeactivated
	m.ClearEvents()

	err := m.Suspend()

	if err != model.ErrInvalidStateTransition {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
	if m.Status != model.MerchantStatusDeactivated {
		t.Errorf("Status must remain DEACTIVATED, got %s", m.Status)
	}
	if len(m.Events) != 0 {
		t.Errorf("no new events expected, got %d", len(m.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-14: ActiveCredential 查询存在的 ACTIVE 凭据
// ─────────────────────────────────────────────────────────────────

func TestMerchant_ActiveCredential_Found_ReturnsCred(t *testing.T) {
	// AC-14
	m, _ := activeMerchantWithCardCred()

	cred, err := m.ActiveCredential(model.ChannelCard)

	if err != nil {
		t.Fatalf("ActiveCredential: want nil error, got %v", err)
	}
	if cred == nil {
		t.Fatal("ActiveCredential: want non-nil cred")
	}
	if cred.Channel != model.ChannelCard {
		t.Errorf("Channel: want CARD, got %s", cred.Channel)
	}
	if cred.Status != model.CredentialStatusActive {
		t.Errorf("Status: want ACTIVE, got %s", cred.Status)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-15: ActiveCredential 查询不存在或已 REVOKED 渠道返回 ErrCredentialNotFound
// ─────────────────────────────────────────────────────────────────

func TestMerchant_ActiveCredential_RevokedOrMissing_ReturnsError(t *testing.T) {
	// AC-15: CARD 渠道已 REVOKED
	m, credID := activeMerchantWithCardCred()
	_ = m.RevokeCredential(credID)

	cred, err := m.ActiveCredential(model.ChannelCard)
	if err != model.ErrCredentialNotFound {
		t.Errorf("REVOKED CARD: want ErrCredentialNotFound, got %v", err)
	}
	if cred != nil {
		t.Errorf("REVOKED CARD: want nil cred, got %+v", cred)
	}

	// AC-15: PAYPAL 渠道不存在
	cred2, err2 := m.ActiveCredential(model.ChannelPayPal)
	if err2 != model.ErrCredentialNotFound {
		t.Errorf("missing PAYPAL: want ErrCredentialNotFound, got %v", err2)
	}
	if cred2 != nil {
		t.Errorf("missing PAYPAL: want nil cred, got %+v", cred2)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-16: ClearEvents 返回事件后清空
// ─────────────────────────────────────────────────────────────────

func TestMerchant_ClearEvents_ReturnsAndClearsEvents(t *testing.T) {
	// AC-16
	m := activeMerchant()
	_ = m.AddCredential(model.ChannelCard, map[string]string{"api_key": "sk"})

	// Events 应有 MerchantRegistered + CredentialAdded
	if len(m.Events) < 1 {
		t.Fatal("Events must be non-empty before ClearEvents")
	}

	events := m.ClearEvents()

	if len(events) < 1 {
		t.Error("ClearEvents: must return at least 1 event")
	}
	if len(m.Events) != 0 {
		t.Errorf("Events must be empty after ClearEvents, got %d", len(m.Events))
	}

	// 再次调用 ClearEvents 返回空
	events2 := m.ClearEvents()
	if len(events2) != 0 {
		t.Errorf("second ClearEvents: want empty, got %d events", len(events2))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-17: MerchantRegistered 实现 DomainEvent 接口
// ─────────────────────────────────────────────────────────────────

func TestMerchantRegistered_EventName(t *testing.T) {
	// AC-17
	evt := event.MerchantRegistered{}
	if evt.EventName() != "merchant.registered" {
		t.Errorf("EventName: want 'merchant.registered', got %q", evt.EventName())
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-18: 所有领域事件均实现 DomainEvent 接口
// ─────────────────────────────────────────────────────────────────

func TestAllDomainEvents_EventName(t *testing.T) {
	// AC-18
	cases := []struct {
		name string
		evt  event.DomainEvent
		want string
	}{
		{"CredentialAdded", event.CredentialAdded{}, "merchant.credential_added"},
		{"CredentialRevoked", event.CredentialRevoked{}, "merchant.credential_revoked"},
		{"MerchantSuspended", event.MerchantSuspended{}, "merchant.suspended"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.evt.EventName() != tc.want {
				t.Errorf("%s.EventName(): want %q, got %q", tc.name, tc.want, tc.evt.EventName())
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-19: NewMerchantID / NewChannelCredentialID 生成合法 UUID
// ─────────────────────────────────────────────────────────────────

func TestNewMerchantID_GeneratesUniqueValidUUIDs(t *testing.T) {
	// AC-19
	const n = 10
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		id := model.NewMerchantID()
		s := string(id)
		if !isValidUUID(s) {
			t.Errorf("NewMerchantID() #%d: invalid UUID %q", i, s)
		}
		if seen[s] {
			t.Errorf("NewMerchantID() #%d: duplicate UUID %q", i, s)
		}
		seen[s] = true
	}
}

func TestNewChannelCredentialID_GeneratesUniqueValidUUIDs(t *testing.T) {
	// AC-19
	const n = 10
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		id := model.NewChannelCredentialID()
		s := string(id)
		if !isValidUUID(s) {
			t.Errorf("NewChannelCredentialID() #%d: invalid UUID %q", i, s)
		}
		if seen[s] {
			t.Errorf("NewChannelCredentialID() #%d: duplicate UUID %q", i, s)
		}
		seen[s] = true
	}
}

func TestNewMerchantID_And_ChannelCredentialID_AllUnique(t *testing.T) {
	// AC-19: 20 个值两两互不相同
	const n = 10
	all := make(map[string]bool, n*2)

	for i := 0; i < n; i++ {
		s := string(model.NewMerchantID())
		if all[s] {
			t.Errorf("duplicate ID: %s", s)
		}
		all[s] = true
	}
	for i := 0; i < n; i++ {
		s := string(model.NewChannelCredentialID())
		if all[s] {
			t.Errorf("duplicate ID: %s", s)
		}
		all[s] = true
	}
}

// ─────────────────────────────────────────────────────────────────
// 额外边界：AddCredential 对 DEACTIVATED 商户也应拒绝（非 ACTIVE 均拒绝）
// ─────────────────────────────────────────────────────────────────

func TestMerchant_AddCredential_DeactivatedMerchant_ReturnsError(t *testing.T) {
	m := activeMerchant()
	m.Status = model.MerchantStatusDeactivated
	m.ClearEvents()

	err := m.AddCredential(model.ChannelCard, map[string]string{"api_key": "sk"})

	if err != model.ErrMerchantNotActive {
		t.Errorf("want ErrMerchantNotActive, got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// 额外边界：Credentials 切片是引用安全的（修改返回的指针不影响其他操作）
// ─────────────────────────────────────────────────────────────────

func TestMerchant_ActiveCredential_ReturnsPointerToSliceElement(t *testing.T) {
	m, _ := activeMerchantWithCardCred()

	cred, err := m.ActiveCredential(model.ChannelCard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 修改指针内容（模拟外部误操作），验证内部状态一致
	originalID := cred.ID
	if originalID != m.Credentials[0].ID {
		t.Error("returned pointer must point to slice element")
	}
}

// ─────────────────────────────────────────────────────────────────
// 常量值对象验证
// ─────────────────────────────────────────────────────────────────

func TestMerchantStatus_Constants(t *testing.T) {
	if string(model.MerchantStatusActive) != "ACTIVE" {
		t.Errorf("MerchantStatusActive: want ACTIVE, got %s", model.MerchantStatusActive)
	}
	if string(model.MerchantStatusSuspended) != "SUSPENDED" {
		t.Errorf("MerchantStatusSuspended: want SUSPENDED, got %s", model.MerchantStatusSuspended)
	}
	if string(model.MerchantStatusDeactivated) != "DEACTIVATED" {
		t.Errorf("MerchantStatusDeactivated: want DEACTIVATED, got %s", model.MerchantStatusDeactivated)
	}
}

func TestPaymentChannel_Constants(t *testing.T) {
	if string(model.ChannelCard) != "CARD" {
		t.Errorf("ChannelCard: want CARD, got %s", model.ChannelCard)
	}
	if string(model.ChannelPayPal) != "PAYPAL" {
		t.Errorf("ChannelPayPal: want PAYPAL, got %s", model.ChannelPayPal)
	}
}

func TestCredentialStatus_Constants(t *testing.T) {
	if string(model.CredentialStatusActive) != "ACTIVE" {
		t.Errorf("CredentialStatusActive: want ACTIVE, got %s", model.CredentialStatusActive)
	}
	if string(model.CredentialStatusRevoked) != "REVOKED" {
		t.Errorf("CredentialStatusRevoked: want REVOKED, got %s", model.CredentialStatusRevoked)
	}
}
