package persistence

import (
	"context"
	"sync"

	"payment-demo/internal/identity/domain/model"
	"payment-demo/internal/identity/domain/port"
)

type InMemoryUserRepository struct {
	mu   sync.RWMutex
	data map[model.UserID]*model.User
}

var _ port.UserRepository = (*InMemoryUserRepository)(nil)

func NewInMemoryUserRepository() *InMemoryUserRepository {
	return &InMemoryUserRepository{
		data: map[model.UserID]*model.User{
			"user_alice": {
				ID:         "user_alice",
				ExternalID: "alice_game_123",
				GameID:     "game_1",
				Status:     model.UserStatusActive,
			},
			"user_bob": {
				ID:         "user_bob",
				ExternalID: "bob_game_456",
				GameID:     "game_1",
				Status:     model.UserStatusActive,
			},
			"user_banned": {
				ID:         "user_banned",
				ExternalID: "banned_789",
				GameID:     "game_1",
				Status:     model.UserStatusBanned,
			},
		},
	}
}

func (r *InMemoryUserRepository) FindByID(_ context.Context, id model.UserID) (*model.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.data[id]
	if !ok {
		return nil, model.ErrUserNotFound
	}
	return u, nil
}
