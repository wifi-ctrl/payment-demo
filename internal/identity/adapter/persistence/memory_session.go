package persistence

import (
	"context"
	"sync"
	"time"

	"payment-demo/internal/identity/domain/model"
	"payment-demo/internal/identity/domain/port"
)

type InMemorySessionRepository struct {
	mu   sync.RWMutex
	data map[string]*model.Session // key = accessToken
}

var _ port.SessionRepository = (*InMemorySessionRepository)(nil)

func NewInMemorySessionRepository() *InMemorySessionRepository {
	return &InMemorySessionRepository{
		data: map[string]*model.Session{
			"token_alice": {
				ID:          "sess_1",
				UserID:      "user_alice",
				AccessToken: "token_alice",
				ExpiresAt:   time.Now().Add(24 * time.Hour),
			},
			"token_bob": {
				ID:          "sess_2",
				UserID:      "user_bob",
				AccessToken: "token_bob",
				ExpiresAt:   time.Now().Add(24 * time.Hour),
			},
			"token_banned": {
				ID:          "sess_3",
				UserID:      "user_banned",
				AccessToken: "token_banned",
				ExpiresAt:   time.Now().Add(24 * time.Hour),
			},
			"token_expired": {
				ID:          "sess_4",
				UserID:      "user_alice",
				AccessToken: "token_expired",
				ExpiresAt:   time.Now().Add(-1 * time.Hour), // 已过期
			},
		},
	}
}

func (r *InMemorySessionRepository) FindByAccessToken(_ context.Context, token string) (*model.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.data[token]
	if !ok {
		return nil, model.ErrInvalidToken
	}
	return s, nil
}
