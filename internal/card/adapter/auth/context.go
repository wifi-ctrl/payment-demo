// Package auth 是 card 上下文与 identity 上下文之间的 ACL 边界。
// 这是 card 包内唯一允许 import identity handler/middleware 的地方。
package auth

import (
	"context"

	identityMW "payment-demo/internal/identity/handler/middleware"
)

// UserIDFromContext 从请求 context 中读取认证后的用户 ID。
// 委托给 identity 中间件的实现，隔离 card/handler 层对 identity 包的直接依赖。
func UserIDFromContext(ctx context.Context) (string, bool) {
	return identityMW.UserIDFromContext(ctx)
}
