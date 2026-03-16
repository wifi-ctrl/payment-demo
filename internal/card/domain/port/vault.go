package port

import (
	"context"

	"payment-demo/internal/card/domain/model"
)

// CardVault 卡信息保险库网关端口（被驱动端口）
// 接收前端一次性 Token，换回可持久存储的 VaultToken + 脱敏信息
// 隔离敏感卡数据，保证 PCI DSS 合规
type CardVault interface {
	// Tokenize 将一次性前端 Token 换取 Vault 持久令牌及脱敏信息
	Tokenize(ctx context.Context, oneTimeToken string) (*VaultResult, error)

	// Delete 从 Vault 删除对应 Token（卡删除时调用，防止泄露）
	Delete(ctx context.Context, token model.VaultToken) error
}

// VaultResult Vault 换取结果 DTO（属于 ACL 边界，放在 port 中）
type VaultResult struct {
	VaultToken model.VaultToken
	Mask       model.CardMask
	Holder     model.CardHolder
}
