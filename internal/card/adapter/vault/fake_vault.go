// Package vault 提供 CardVault 的非生产环境 Fake 实现。
// FakeCardVault 仅限 Dev / Staging / 测试环境使用，禁止在生产环境部署。
package vault

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"strings"

	"payment-demo/internal/card/domain/model"
	"payment-demo/internal/card/domain/port"
)

// FakeCardVault 模拟第三方卡信息保险库（如 Stripe Vault）。
// 这是 ACL 边界 — 在此完成一次性 Token → 持久 VaultToken 的翻译。
//
// 注意：仅限非生产环境。
type FakeCardVault struct{}

// 编译期检查接口实现
var _ port.CardVault = (*FakeCardVault)(nil)

func NewFakeCardVault() *FakeCardVault {
	return &FakeCardVault{}
}

// Tokenize 将前端一次性 Token 换取 Vault 持久令牌及脱敏信息。
// 约定：以 "tok_fail" 开头的 Token 模拟失败场景。
func (v *FakeCardVault) Tokenize(_ context.Context, oneTimeToken string) (*port.VaultResult, error) {
	log.Printf("[FakeVault] Tokenize: oneTimeToken=%s", oneTimeToken)

	if strings.HasPrefix(oneTimeToken, "tok_fail") {
		return nil, fmt.Errorf("vault tokenize rejected: invalid token")
	}

	// 使用 crypto/rand 生成随机 token，避免可预测性
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("vault token generation failed: %w", err)
	}
	vaultToken := model.VaultToken{
		Token:    fmt.Sprintf("vault_%x", b),
		Provider: "fake",
	}

	// 根据前缀模拟不同卡品牌
	brand := "Visa"
	last4 := "4242"
	if strings.HasPrefix(oneTimeToken, "tok_mc") {
		brand = "Mastercard"
		last4 = "5353"
	} else if strings.HasPrefix(oneTimeToken, "tok_up") {
		brand = "UnionPay"
		last4 = "6200"
	}

	return &port.VaultResult{
		VaultToken: vaultToken,
		Mask: model.CardMask{
			Last4:       last4,
			Brand:       brand,
			ExpireMonth: 12,
			ExpireYear:  2028,
		},
		Holder: model.CardHolder{
			Name:           "Card Holder",
			BillingCountry: "US",
		},
	}, nil
}

// Delete 从 Vault 删除对应 Token
func (v *FakeCardVault) Delete(_ context.Context, token model.VaultToken) error {
	log.Printf("[FakeVault] Delete: vaultToken=%s, provider=%s", token.Token, token.Provider)
	return nil
}
