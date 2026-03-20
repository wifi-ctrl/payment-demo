package vault

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"payment-demo/internal/card/domain/model"
	"payment-demo/internal/card/domain/port"
)

type cachedEntry struct {
	data      port.CachedCardData
	expiresAt time.Time
}

// LocalVault 本地内存 CardVault 实现（Demo 用）
type LocalVault struct {
	mu    sync.Mutex
	cache map[string]*cachedEntry
	ttl   time.Duration
}

var _ port.CardVault = (*LocalVault)(nil)

func NewLocalVault() *LocalVault {
	return &LocalVault{
		cache: make(map[string]*cachedEntry),
		ttl:   15 * time.Minute,
	}
}

func (v *LocalVault) CacheTokenizedCard(_ context.Context, data port.CachedCardData) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	token := "ct_" + uuid.New().String()
	v.cache[token] = &cachedEntry{
		data:      data,
		expiresAt: time.Now().Add(v.ttl),
	}
	return token, nil
}

func (v *LocalVault) PeekCachedCard(_ context.Context, cardToken, userID string) (*port.CachedCardData, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	entry, ok := v.cache[cardToken]
	if !ok {
		return nil, model.ErrCardTokenInvalid
	}
	if time.Now().After(entry.expiresAt) {
		return nil, model.ErrCardTokenExpired
	}
	if entry.data.UserID != userID {
		return nil, model.ErrCardBelongsToOtherUser
	}
	cp := entry.data
	return &cp, nil
}

func (v *LocalVault) ConsumeCardToken(_ context.Context, cardToken string) (*port.CachedCardData, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	entry, ok := v.cache[cardToken]
	if !ok {
		return nil, model.ErrCardTokenInvalid
	}
	delete(v.cache, cardToken)
	if time.Now().After(entry.expiresAt) {
		return nil, model.ErrCardTokenExpired
	}
	return &entry.data, nil
}
