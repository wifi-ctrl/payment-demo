package merchant_test

import (
	"context"
	"errors"
	"testing"

	merchantAdapter "payment-demo/internal/payment/adapter/merchant"
	merchantModel "payment-demo/internal/merchant/domain/model"
	merchantPort "payment-demo/internal/merchant/domain/port"
	paymentModel "payment-demo/internal/payment/domain/model"
	"payment-demo/internal/payment/domain/port"
)

// ─────────────────────────────────────────────────────────────────
// stub: merchant 侧的 MerchantRepository 替身
// ─────────────────────────────────────────────────────────────────

type stubMerchantRepository struct {
	merchants map[merchantModel.MerchantID]*merchantModel.Merchant
	findErr   error // 非 nil 时 FindByID 返回此 error
}

var _ merchantPort.MerchantRepository = (*stubMerchantRepository)(nil)

func newStubMerchantRepository() *stubMerchantRepository {
	return &stubMerchantRepository{
		merchants: make(map[merchantModel.MerchantID]*merchantModel.Merchant),
	}
}

func (r *stubMerchantRepository) Save(_ context.Context, m *merchantModel.Merchant) error {
	r.merchants[m.ID] = m
	return nil
}

func (r *stubMerchantRepository) FindByID(_ context.Context, id merchantModel.MerchantID) (*merchantModel.Merchant, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	m, ok := r.merchants[id]
	if !ok {
		return nil, merchantModel.ErrMerchantNotFound
	}
	return m, nil
}

func (r *stubMerchantRepository) FindAll(_ context.Context) ([]*merchantModel.Merchant, error) {
	result := make([]*merchantModel.Merchant, 0, len(r.merchants))
	for _, m := range r.merchants {
		result = append(result, m)
	}
	return result, nil
}

// ─────────────────────────────────────────────────────────────────
// 测试辅助：构造带 CARD ACTIVE 凭据的商户
// ─────────────────────────────────────────────────────────────────

// seedActiveMerchantWithCard 在仓储中放置一个 ACTIVE 商户（含 CARD ACTIVE 凭据）
// 返回商户 ID 和凭据 ID
func seedActiveMerchantWithCard(repo *stubMerchantRepository, secrets map[string]string) (merchantModel.MerchantID, merchantModel.ChannelCredentialID) {
	m := merchantModel.NewMerchant("Acme Corp")
	m.ClearEvents()
	_ = m.AddCredential(merchantModel.ChannelCard, secrets)
	credID := m.Credentials[0].ID
	m.ClearEvents()
	repo.merchants[m.ID] = m
	return m.ID, credID
}

// ─────────────────────────────────────────────────────────────────
// AC-46: MerchantAdapter.FindActiveCredential 翻译成功
// ─────────────────────────────────────────────────────────────────

func TestMerchantAdapter_FindActiveCredential_Success(t *testing.T) {
	// AC-46
	repo := newStubMerchantRepository()
	secrets := map[string]string{"api_key": "sk"}
	mid, credID := seedActiveMerchantWithCard(repo, secrets)

	adapter := merchantAdapter.NewMerchantAdapter(repo)

	view, err := adapter.FindActiveCredential(context.Background(), string(mid), paymentModel.PaymentMethodCard)

	if err != nil {
		t.Fatalf("FindActiveCredential: want nil error, got %v", err)
	}
	if view == nil {
		t.Fatal("FindActiveCredential: want non-nil view")
	}
	if view.CredentialID != string(credID) {
		t.Errorf("CredentialID: want %s, got %s", credID, view.CredentialID)
	}
	if view.MerchantID != string(mid) {
		t.Errorf("MerchantID: want %s, got %s", mid, view.MerchantID)
	}
	if view.Channel != "CARD" {
		t.Errorf("Channel: want CARD, got %s", view.Channel)
	}
	if view.Secrets["api_key"] != "sk" {
		t.Errorf("Secrets[api_key]: want sk, got %s", view.Secrets["api_key"])
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-47: MerchantAdapter.FindActiveCredential 商户不存在返回错误
// ─────────────────────────────────────────────────────────────────

func TestMerchantAdapter_FindActiveCredential_MerchantNotFound_ReturnsError(t *testing.T) {
	// AC-47
	repo := newStubMerchantRepository()
	// 不放入任何商户
	adapter := merchantAdapter.NewMerchantAdapter(repo)

	view, err := adapter.FindActiveCredential(context.Background(), "non-existent", paymentModel.PaymentMethodCard)

	if !errors.Is(err, port.ErrMerchantCredentialNotFound) {
		t.Errorf("want ErrMerchantCredentialNotFound, got %v", err)
	}
	if view != nil {
		t.Errorf("want nil view, got %+v", view)
	}
}

// ─────────────────────────────────────────────────────────────────
// AC-48: MerchantAdapter.FindActiveCredential 渠道凭据不存在或已 REVOKED 返回错误
// ─────────────────────────────────────────────────────────────────

func TestMerchantAdapter_FindActiveCredential_ChannelNotFound_ReturnsError(t *testing.T) {
	// AC-48: 商户存在但无 PAYPAL 凭据
	repo := newStubMerchantRepository()
	mid, _ := seedActiveMerchantWithCard(repo, map[string]string{"api_key": "sk"})
	adapter := merchantAdapter.NewMerchantAdapter(repo)

	view, err := adapter.FindActiveCredential(context.Background(), string(mid), paymentModel.PaymentMethodPayPal)

	if !errors.Is(err, port.ErrMerchantCredentialNotFound) {
		t.Errorf("want ErrMerchantCredentialNotFound, got %v", err)
	}
	if view != nil {
		t.Errorf("want nil view, got %+v", view)
	}
}

func TestMerchantAdapter_FindActiveCredential_RevokedCredential_ReturnsError(t *testing.T) {
	// AC-48: CARD 凭据已 REVOKED
	repo := newStubMerchantRepository()
	mid, credID := seedActiveMerchantWithCard(repo, map[string]string{"api_key": "sk"})

	// 吊销凭据
	m := repo.merchants[mid]
	_ = m.RevokeCredential(credID)
	m.ClearEvents()

	adapter := merchantAdapter.NewMerchantAdapter(repo)

	view, err := adapter.FindActiveCredential(context.Background(), string(mid), paymentModel.PaymentMethodCard)

	if !errors.Is(err, port.ErrMerchantCredentialNotFound) {
		t.Errorf("want ErrMerchantCredentialNotFound, got %v", err)
	}
	if view != nil {
		t.Errorf("want nil view, got %+v", view)
	}
}

func TestMerchantAdapter_FindActiveCredential_SuspendedMerchant_ReturnsError(t *testing.T) {
	// AC-48 附加：商户已暂停，凭据不可用
	repo := newStubMerchantRepository()
	mid, _ := seedActiveMerchantWithCard(repo, map[string]string{"api_key": "sk"})

	// 暂停商户
	m := repo.merchants[mid]
	_ = m.Suspend()
	m.ClearEvents()

	adapter := merchantAdapter.NewMerchantAdapter(repo)

	view, err := adapter.FindActiveCredential(context.Background(), string(mid), paymentModel.PaymentMethodCard)

	if !errors.Is(err, port.ErrMerchantCredentialNotFound) {
		t.Errorf("suspended merchant: want ErrMerchantCredentialNotFound, got %v", err)
	}
	if view != nil {
		t.Errorf("want nil view, got %+v", view)
	}
}

// ─────────────────────────────────────────────────────────────────
// 额外：FindActiveCredential 对 PayPal 凭据翻译正确
// ─────────────────────────────────────────────────────────────────

func TestMerchantAdapter_FindActiveCredential_PayPalChannel_Success(t *testing.T) {
	repo := newStubMerchantRepository()
	m := merchantModel.NewMerchant("PayPal Corp")
	m.ClearEvents()
	_ = m.AddCredential(merchantModel.ChannelPayPal, map[string]string{
		"client_id":     "paypal-client-id",
		"client_secret": "paypal-secret",
	})
	credID := m.Credentials[0].ID
	m.ClearEvents()
	repo.merchants[m.ID] = m

	adapter := merchantAdapter.NewMerchantAdapter(repo)

	view, err := adapter.FindActiveCredential(context.Background(), string(m.ID), paymentModel.PaymentMethodPayPal)

	if err != nil {
		t.Fatalf("FindActiveCredential PayPal: want nil error, got %v", err)
	}
	if view.Channel != "PAYPAL" {
		t.Errorf("Channel: want PAYPAL, got %s", view.Channel)
	}
	if view.CredentialID != string(credID) {
		t.Errorf("CredentialID: want %s, got %s", credID, view.CredentialID)
	}
	if view.Secrets["client_id"] != "paypal-client-id" {
		t.Errorf("Secrets[client_id]: want paypal-client-id, got %s", view.Secrets["client_id"])
	}
	if view.Secrets["client_secret"] != "paypal-secret" {
		t.Errorf("Secrets[client_secret]: want paypal-secret, got %s", view.Secrets["client_secret"])
	}
}
