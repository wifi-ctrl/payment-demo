package application_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"payment-demo/internal/merchant/application"
	"payment-demo/internal/merchant/domain/model"
)

// ─────────────────────────────────────────────────────────────────
// stub 替身：可控行为 + 可观测调用
// ─────────────────────────────────────────────────────────────────

// stubMerchantRepo 可控的内存仓储替身。
// Save 在 saveErr==nil 时将入参追加到 saveArgs 并计数；
// FindByID 在 findErr!=nil 时返回该错误，否则从 store 中查找。
type stubMerchantRepo struct {
	mu        sync.Mutex
	store     map[model.MerchantID]*model.Merchant
	saveErr   error
	findErr   error
	saveCalls int
	saveArgs  []*model.Merchant
}

func newStubRepo() *stubMerchantRepo {
	return &stubMerchantRepo{
		store: make(map[model.MerchantID]*model.Merchant),
	}
}

func (r *stubMerchantRepo) Save(_ context.Context, m *model.Merchant) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saveCalls++
	r.saveArgs = append(r.saveArgs, m)
	r.store[m.ID] = m
	return nil
}

func (r *stubMerchantRepo) FindByID(_ context.Context, id model.MerchantID) (*model.Merchant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.findErr != nil {
		return nil, r.findErr
	}
	m, ok := r.store[id]
	if !ok {
		return nil, model.ErrMerchantNotFound
	}
	return m, nil
}

func (r *stubMerchantRepo) FindAll(_ context.Context) ([]*model.Merchant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*model.Merchant, 0, len(r.store))
	for _, m := range r.store {
		result = append(result, m)
	}
	return result, nil
}

// resetSave 重置 Save 观测状态（seed 操作直接写 store，不经 Save）
func (r *stubMerchantRepo) resetSave() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saveCalls = 0
	r.saveArgs = nil
}

// ─────────────────────────────────────────────────────────────────
// 测试辅助
// ─────────────────────────────────────────────────────────────────

// seedMerchant 直接写入 store，不经 Save（不影响 saveCalls 计数）
func seedMerchant(repo *stubMerchantRepo, name string) *model.Merchant {
	m := model.NewMerchant(name)
	m.ClearEvents()
	repo.store[m.ID] = m
	return m
}

// seedMerchantWithCard 预置 ACTIVE 商户 + CARD ACTIVE 凭据，返回商户和凭据 ID
func seedMerchantWithCard(repo *stubMerchantRepo) (*model.Merchant, model.ChannelCredentialID) {
	m := seedMerchant(repo, "Acme Corp")
	_ = m.AddCredential(model.ChannelCard, map[string]string{"api_key": "sk_live_xxx"})
	credID := m.Credentials[0].ID
	m.ClearEvents()
	return m, credID
}

// ─────────────────────────────────────────────────────────────────
// AC-20: RegisterMerchant 正常注册并持久化
// ─────────────────────────────────────────────────────────────────

func TestMerchantUseCase_Register_Success(t *testing.T) {
	// AC-20
	repo := newStubRepo()
	uc := application.NewMerchantUseCase(repo)

	m, err := uc.Register(context.Background(), application.RegisterRequest{Name: "Acme Corp"})

	if err != nil {
		t.Fatalf("Register: want nil error, got %v", err)
	}
	if m == nil {
		t.Fatal("Register: want non-nil merchant")
	}
	if m.Status != model.MerchantStatusActive {
		t.Errorf("Status: want ACTIVE, got %s", m.Status)
	}
	// Save 被调用一次
	if repo.saveCalls != 1 {
		t.Errorf("Save calls: want 1, got %d", repo.saveCalls)
	}
	// Save 入参与返回 Merchant 是同一个指针
	if repo.saveArgs[0] != m {
		t.Error("Save arg must be the returned Merchant")
	}
	// ClearEvents 已被调用（Domain Events 已清空）
	if len(m.Events) != 0 {
		t.Errorf("Events must be cleared after Register, got %d", len(m.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-21: RegisterMerchant 持久化失败不泄漏商户对象
// ─────────────────────────────────────────────────────────────────

func TestMerchantUseCase_Register_SaveFails_ReturnsNil(t *testing.T) {
	// AC-21
	repo := newStubRepo()
	repo.saveErr = errors.New("db connection lost")
	uc := application.NewMerchantUseCase(repo)

	m, err := uc.Register(context.Background(), application.RegisterRequest{Name: "Acme Corp"})

	if err == nil {
		t.Error("Register: want error, got nil")
	}
	if m != nil {
		t.Errorf("Register: want nil merchant on error, got %+v", m)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-22: AddCredential 用例正常添加并持久化
// ─────────────────────────────────────────────────────────────────

func TestMerchantUseCase_AddCredential_Success(t *testing.T) {
	// AC-22
	repo := newStubRepo()
	m := seedMerchant(repo, "Acme Corp")
	uc := application.NewMerchantUseCase(repo)

	_, err := uc.AddCredential(context.Background(), application.AddCredentialRequest{
		MerchantID: string(m.ID),
		Channel:    model.ChannelCard,
		Secrets:    map[string]string{"api_key": "sk_live_xxx"},
	})

	if err != nil {
		t.Fatalf("AddCredential: want nil error, got %v", err)
	}
	// Save 被调用一次
	if repo.saveCalls != 1 {
		t.Errorf("Save calls: want 1, got %d", repo.saveCalls)
	}
	// 持久化的商户包含新 ACTIVE 凭据
	saved := repo.saveArgs[0]
	if len(saved.Credentials) != 1 {
		t.Fatalf("Credentials len: want 1, got %d", len(saved.Credentials))
	}
	if saved.Credentials[0].Status != model.CredentialStatusActive {
		t.Errorf("Credential status: want ACTIVE, got %s", saved.Credentials[0].Status)
	}
	// Domain Events 已被 ClearEvents() 清空
	if len(saved.Events) != 0 {
		t.Errorf("Events must be cleared, got %d", len(saved.Events))
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-23: AddCredential 商户不存在返回错误，不调用 Save
// ─────────────────────────────────────────────────────────────────

func TestMerchantUseCase_AddCredential_MerchantNotFound_DoesNotSave(t *testing.T) {
	// AC-23
	repo := newStubRepo()
	repo.findErr = model.ErrMerchantNotFound
	uc := application.NewMerchantUseCase(repo)

	_, err := uc.AddCredential(context.Background(), application.AddCredentialRequest{
		MerchantID: "non-existent-id",
		Channel:    model.ChannelCard,
		Secrets:    map[string]string{"api_key": "sk"},
	})

	if !errors.Is(err, model.ErrMerchantNotFound) {
		t.Errorf("want ErrMerchantNotFound, got %v", err)
	}
	if repo.saveCalls != 0 {
		t.Errorf("Save must not be called, got %d calls", repo.saveCalls)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-24: AddCredential 领域规则冲突（已有 ACTIVE 凭据）不调用 Save
// ─────────────────────────────────────────────────────────────────

func TestMerchantUseCase_AddCredential_DuplicateActive_DoesNotSave(t *testing.T) {
	// AC-24
	repo := newStubRepo()
	m, _ := seedMerchantWithCard(repo) // 已有 CARD ACTIVE 凭据，seed 直接写 store
	repo.resetSave()                   // 重置计数（seed 不经过 Save）
	uc := application.NewMerchantUseCase(repo)

	_, err := uc.AddCredential(context.Background(), application.AddCredentialRequest{
		MerchantID: string(m.ID),
		Channel:    model.ChannelCard,
		Secrets:    map[string]string{"api_key": "new_key"},
	})

	if !errors.Is(err, model.ErrCredentialAlreadyExists) {
		t.Errorf("want ErrCredentialAlreadyExists, got %v", err)
	}
	if repo.saveCalls != 0 {
		t.Errorf("Save must not be called on domain error, got %d calls", repo.saveCalls)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-25: RevokeCredential 用例正常吊销并持久化
// ─────────────────────────────────────────────────────────────────

func TestMerchantUseCase_RevokeCredential_Success(t *testing.T) {
	// AC-25
	repo := newStubRepo()
	m, credID := seedMerchantWithCard(repo)
	repo.resetSave()
	uc := application.NewMerchantUseCase(repo)

	_, err := uc.RevokeCredential(context.Background(), application.RevokeCredentialRequest{
		MerchantID:   string(m.ID),
		CredentialID: string(credID),
	})

	if err != nil {
		t.Fatalf("RevokeCredential: want nil error, got %v", err)
	}
	if repo.saveCalls != 1 {
		t.Errorf("Save calls: want 1, got %d", repo.saveCalls)
	}
	// 持久化的商户中凭据状态为 REVOKED
	saved := repo.saveArgs[0]
	if saved.Credentials[0].Status != model.CredentialStatusRevoked {
		t.Errorf("Credential status: want REVOKED, got %s", saved.Credentials[0].Status)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-26: SuspendMerchant 用例正常暂停并持久化
// ─────────────────────────────────────────────────────────────────

func TestMerchantUseCase_Suspend_Success(t *testing.T) {
	// AC-26
	repo := newStubRepo()
	m := seedMerchant(repo, "Acme Corp")
	repo.resetSave()
	uc := application.NewMerchantUseCase(repo)

	_, err := uc.Suspend(context.Background(), string(m.ID))

	if err != nil {
		t.Fatalf("Suspend: want nil error, got %v", err)
	}
	if repo.saveCalls != 1 {
		t.Errorf("Save calls: want 1, got %d", repo.saveCalls)
	}
	saved := repo.saveArgs[0]
	if saved.Status != model.MerchantStatusSuspended {
		t.Errorf("Status: want SUSPENDED, got %s", saved.Status)
	}
}

// ─────────────────────────────────────────────────────────────────
// 边界：RevokeCredential 商户不存在时不调用 Save
// ─────────────────────────────────────────────────────────────────

func TestMerchantUseCase_RevokeCredential_MerchantNotFound_DoesNotSave(t *testing.T) {
	repo := newStubRepo()
	repo.findErr = model.ErrMerchantNotFound
	uc := application.NewMerchantUseCase(repo)

	_, err := uc.RevokeCredential(context.Background(), application.RevokeCredentialRequest{
		MerchantID:   "non-existent",
		CredentialID: "cred-1",
	})

	if err == nil {
		t.Error("want error, got nil")
	}
	if repo.saveCalls != 0 {
		t.Errorf("Save must not be called, got %d calls", repo.saveCalls)
	}
}

// ─────────────────────────────────────────────────────────────────
// 边界：Suspend 商户不存在时不调用 Save
// ─────────────────────────────────────────────────────────────────

func TestMerchantUseCase_Suspend_MerchantNotFound_DoesNotSave(t *testing.T) {
	repo := newStubRepo()
	repo.findErr = model.ErrMerchantNotFound
	uc := application.NewMerchantUseCase(repo)

	_, err := uc.Suspend(context.Background(), "non-existent")

	if err == nil {
		t.Error("want error, got nil")
	}
	if repo.saveCalls != 0 {
		t.Errorf("Save must not be called, got %d calls", repo.saveCalls)
	}
}

// ─────────────────────────────────────────────────────────────────
// 边界：Suspend 已暂停商户，返回 ErrInvalidStateTransition，Save 不被调用
// ─────────────────────────────────────────────────────────────────

func TestMerchantUseCase_Suspend_AlreadySuspended_DoesNotSave(t *testing.T) {
	repo := newStubRepo()
	m := seedMerchant(repo, "Acme Corp")
	_ = m.Suspend()
	m.ClearEvents()
	repo.resetSave()
	uc := application.NewMerchantUseCase(repo)

	_, err := uc.Suspend(context.Background(), string(m.ID))

	if !errors.Is(err, model.ErrInvalidStateTransition) {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
	if repo.saveCalls != 0 {
		t.Errorf("Save must not be called on domain error, got %d calls", repo.saveCalls)
	}
}

// ─────────────────────────────────────────────────────────────────
// 边界：RevokeCredential 凭据不存在返回 ErrCredentialNotFound，Save 不被调用
// ─────────────────────────────────────────────────────────────────

func TestMerchantUseCase_RevokeCredential_CredentialNotFound_DoesNotSave(t *testing.T) {
	repo := newStubRepo()
	m := seedMerchant(repo, "Acme Corp") // 无凭据
	repo.resetSave()
	uc := application.NewMerchantUseCase(repo)

	_, err := uc.RevokeCredential(context.Background(), application.RevokeCredentialRequest{
		MerchantID:   string(m.ID),
		CredentialID: "non-existent-cred",
	})

	if !errors.Is(err, model.ErrCredentialNotFound) {
		t.Errorf("want ErrCredentialNotFound, got %v", err)
	}
	if repo.saveCalls != 0 {
		t.Errorf("Save must not be called, got %d calls", repo.saveCalls)
	}
}

// ─────────────────────────────────────────────────────────────────
// 边界：GetMerchant 正常查询
// ─────────────────────────────────────────────────────────────────

func TestMerchantUseCase_GetMerchant_Success(t *testing.T) {
	repo := newStubRepo()
	m := seedMerchant(repo, "Test Corp")
	uc := application.NewMerchantUseCase(repo)

	got, err := uc.GetMerchant(context.Background(), string(m.ID))

	if err != nil {
		t.Fatalf("GetMerchant: want nil error, got %v", err)
	}
	if got.ID != m.ID {
		t.Errorf("ID: want %s, got %s", m.ID, got.ID)
	}
}

func TestMerchantUseCase_GetMerchant_NotFound(t *testing.T) {
	repo := newStubRepo()
	uc := application.NewMerchantUseCase(repo)

	_, err := uc.GetMerchant(context.Background(), "no-such-id")

	if !errors.Is(err, model.ErrMerchantNotFound) {
		t.Errorf("want ErrMerchantNotFound, got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// 边界：ListMerchants 返回所有商户
// ─────────────────────────────────────────────────────────────────

func TestMerchantUseCase_ListMerchants_ReturnsAll(t *testing.T) {
	repo := newStubRepo()
	seedMerchant(repo, "A")
	seedMerchant(repo, "B")
	seedMerchant(repo, "C")
	uc := application.NewMerchantUseCase(repo)

	merchants, err := uc.ListMerchants(context.Background())

	if err != nil {
		t.Fatalf("ListMerchants: want nil error, got %v", err)
	}
	if len(merchants) != 3 {
		t.Errorf("len: want 3, got %d", len(merchants))
	}
}

func TestMerchantUseCase_ListMerchants_EmptyRepo_ReturnsEmpty(t *testing.T) {
	repo := newStubRepo()
	uc := application.NewMerchantUseCase(repo)

	merchants, err := uc.ListMerchants(context.Background())

	if err != nil {
		t.Fatalf("ListMerchants: want nil error, got %v", err)
	}
	if len(merchants) != 0 {
		t.Errorf("len: want 0, got %d", len(merchants))
	}
}

// ─────────────────────────────────────────────────────────────────
// 边界：AddCredential 非 ACTIVE 商户拒绝，Save 不被调用
// ─────────────────────────────────────────────────────────────────

func TestMerchantUseCase_AddCredential_SuspendedMerchant_DoesNotSave(t *testing.T) {
	repo := newStubRepo()
	m := seedMerchant(repo, "Acme Corp")
	_ = m.Suspend()
	m.ClearEvents()
	repo.resetSave()
	uc := application.NewMerchantUseCase(repo)

	_, err := uc.AddCredential(context.Background(), application.AddCredentialRequest{
		MerchantID: string(m.ID),
		Channel:    model.ChannelCard,
		Secrets:    map[string]string{"api_key": "sk"},
	})

	if !errors.Is(err, model.ErrMerchantNotActive) {
		t.Errorf("want ErrMerchantNotActive, got %v", err)
	}
	if repo.saveCalls != 0 {
		t.Errorf("Save must not be called, got %d calls", repo.saveCalls)
	}
}
