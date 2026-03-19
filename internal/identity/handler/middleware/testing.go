package middleware

import "payment-demo/internal/shared/auth"

// WithUserID 将 userID 写入 context，供测试使用。
// 委托给 shared/auth，保持向后兼容。
var WithUserID = auth.WithUserID
