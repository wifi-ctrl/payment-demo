package vault

import (
	"context"
	"fmt"
	"log"

	"payment-demo/internal/card/domain/model"
	"payment-demo/internal/card/domain/port"
	"payment-demo/internal/infra/stripe"
)

// StripeVaultAdapter 基于 Stripe Tokens API 的 CardVault 实现。
// 通过共享的 stripe.Client 与 Stripe 通信，将 Stripe 响应翻译为 card 领域模型（ACL）。
//
// adapter 只做 ACL 翻译（stripe.TokenResult → port.VaultResult），
// 不关心 HTTP URL、请求格式等 Stripe API 细节 — 那些封装在 stripe.Client 中。
type StripeVaultAdapter struct {
	client *stripe.Client
}

var _ port.CardVault = (*StripeVaultAdapter)(nil)

// NewStripeVaultAdapter 构造函数，注入共享的 Stripe 客户端。
func NewStripeVaultAdapter(client *stripe.Client) *StripeVaultAdapter {
	return &StripeVaultAdapter{client: client}
}

// Tokenize 调用 Stripe Tokens API 将一次性 Token 换取持久令牌 + 脱敏信息。
// ACL 翻译：stripe.TokenResult → port.VaultResult（card 领域模型）。
func (a *StripeVaultAdapter) Tokenize(_ context.Context, oneTimeToken string) (*port.VaultResult, error) {
	result, err := a.client.CreateToken(stripe.TokenParams{
		OneTimeToken: oneTimeToken,
	})
	if err != nil {
		return nil, fmt.Errorf("stripe vault tokenize failed: %w", err)
	}

	// ACL 翻译：stripe.TokenResult → card 领域值对象
	return &port.VaultResult{
		VaultToken: model.VaultToken{
			Token:    result.ID,
			Provider: "stripe",
		},
		Mask: model.CardMask{
			Last4:       result.CardLast4,
			Brand:       result.CardBrand,
			ExpireMonth: result.ExpMonth,
			ExpireYear:  result.ExpYear,
		},
		Holder: model.CardHolder{
			Name:           "Card Holder",
			BillingCountry: "US",
		},
	}, nil
}

// Delete 调用 Stripe API 删除 Vault 中的 Token。
func (a *StripeVaultAdapter) Delete(_ context.Context, token model.VaultToken) error {
	if err := a.client.DeleteToken(token.Token); err != nil {
		log.Printf("[StripeVault] Delete failed: token=%s, err=%v", token.Token, err)
		return err
	}
	return nil
}
