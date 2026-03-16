package application

import (
	"context"
	"time"

	"payment-demo/internal/identity/domain/model"
	"payment-demo/internal/identity/domain/port"
)

// AuthUseCase 认证用例
type AuthUseCase struct {
	users    port.UserRepository
	sessions port.SessionRepository
}

func NewAuthUseCase(users port.UserRepository, sessions port.SessionRepository) *AuthUseCase {
	return &AuthUseCase{users: users, sessions: sessions}
}

// Authenticate 验证 access token，返回用户信息
func (uc *AuthUseCase) Authenticate(ctx context.Context, accessToken string) (*model.User, error) {
	session, err := uc.sessions.FindByAccessToken(ctx, accessToken)
	if err != nil {
		return nil, model.ErrInvalidToken
	}
	if session.IsExpired(time.Now()) {
		return nil, model.ErrSessionExpired
	}

	user, err := uc.users.FindByID(ctx, session.UserID)
	if err != nil {
		return nil, err
	}
	if user.IsBanned() {
		return nil, model.ErrUserBanned
	}

	return user, nil
}
