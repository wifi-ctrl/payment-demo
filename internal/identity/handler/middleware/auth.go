package middleware

import (
	"net/http"
	"strings"

	"payment-demo/internal/identity/application"
	"payment-demo/internal/shared/auth"
	"payment-demo/internal/shared/httputil"
)

// UserIDFromContext 从 context 中取出认证后的用户 ID。
// 委托给 shared/auth，保持向后兼容。
var UserIDFromContext = auth.UserIDFromContext

// AuthMiddleware 认证中间件
// 依赖 identity 上下文的 AuthUseCase
type AuthMiddleware struct {
	authUseCase *application.AuthUseCase
}

func NewAuthMiddleware(authUseCase *application.AuthUseCase) *AuthMiddleware {
	return &AuthMiddleware{authUseCase: authUseCase}
}

func (m *AuthMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			httputil.Error(w, "missing authorization token", http.StatusUnauthorized)
			return
		}

		user, err := m.authUseCase.Authenticate(r.Context(), token)
		if err != nil {
			httputil.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		// 只往 ctx 写 userID（string），不泄漏 identity 的领域模型
		ctx := auth.WithUserID(r.Context(), string(user.ID))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
