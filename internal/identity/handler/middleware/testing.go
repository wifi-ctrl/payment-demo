package middleware

import "context"

// WithUserID 将 userID 写入 context，供测试使用。
// 生产代码应通过 AuthMiddleware 注入；此函数仅供测试构造携带认证信息的请求。
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}
