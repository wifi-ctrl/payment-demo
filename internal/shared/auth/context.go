// Package auth 提供跨上下文共享的用户认证 context 读写函数。
// identity 中间件写入 UserID，其他上下文的 handler 读取。
package auth

import "context"

type contextKey string

const userIDKey contextKey = "user_id"

// UserIDFromContext 从 context 中取出认证后的用户 ID。
func UserIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(userIDKey).(string)
	return id, ok
}

// WithUserID 将 userID 写入 context。
// 生产代码由 identity AuthMiddleware 调用；测试代码直接调用。
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}
