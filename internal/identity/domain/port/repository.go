package port

import (
	"context"

	"payment-demo/internal/identity/domain/model"
)

// UserRepository 用户仓储端口
type UserRepository interface {
	FindByID(ctx context.Context, id model.UserID) (*model.User, error)
}

// SessionRepository 会话仓储端口
type SessionRepository interface {
	FindByAccessToken(ctx context.Context, token string) (*model.Session, error)
}
