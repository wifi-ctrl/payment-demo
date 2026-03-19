package persistence

import (
	"context"
	"sync"

	"payment-demo/internal/card/domain/model"
	"payment-demo/internal/card/domain/port"
)

// InMemoryCardRepository 内存卡仓储实现
type InMemoryCardRepository struct {
	mu   sync.RWMutex
	data map[model.SavedCardID]*model.SavedCard
}

var _ port.CardRepository = (*InMemoryCardRepository)(nil)

func NewInMemoryCardRepository() *InMemoryCardRepository {
	return &InMemoryCardRepository{
		data: make(map[model.SavedCardID]*model.SavedCard),
	}
}

func clone(card *model.SavedCard) *model.SavedCard {
	c := *card
	c.Events = nil
	if card.EncryptedPAN.Ciphertext != nil {
		c.EncryptedPAN.Ciphertext = make([]byte, len(card.EncryptedPAN.Ciphertext))
		copy(c.EncryptedPAN.Ciphertext, card.EncryptedPAN.Ciphertext)
	}
	if card.ChannelTokens != nil {
		c.ChannelTokens = make([]model.ChannelToken, len(card.ChannelTokens))
		copy(c.ChannelTokens, card.ChannelTokens)
	}
	return &c
}

func (r *InMemoryCardRepository) Save(_ context.Context, card *model.SavedCard) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[card.ID] = clone(card)
	return nil
}

func (r *InMemoryCardRepository) FindByID(_ context.Context, id model.SavedCardID) (*model.SavedCard, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	card, ok := r.data[id]
	if !ok {
		return nil, model.ErrCardNotFound
	}
	return clone(card), nil
}

func (r *InMemoryCardRepository) FindAllByUserID(_ context.Context, userID string) ([]*model.SavedCard, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*model.SavedCard
	for _, card := range r.data {
		if card.UserID == userID && card.Status != model.CardStatusDeleted {
			result = append(result, clone(card))
		}
	}
	return result, nil
}

func (r *InMemoryCardRepository) FindDefaultByUserID(_ context.Context, userID string) (*model.SavedCard, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, card := range r.data {
		if card.UserID == userID && card.IsDefault && card.Status != model.CardStatusDeleted {
			return clone(card), nil
		}
	}
	return nil, nil
}

func (r *InMemoryCardRepository) FindActiveByUserAndPANHash(
	_ context.Context, userID string, panHash model.PANHash,
) (*model.SavedCard, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, card := range r.data {
		if card.UserID == userID && card.PANHash == panHash && card.Status == model.CardStatusActive {
			return clone(card), nil
		}
	}
	return nil, model.ErrCardNotFound
}

func (r *InMemoryCardRepository) FindByKeyVersion(
	_ context.Context, keyVersion int,
) ([]*model.SavedCard, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*model.SavedCard
	for _, card := range r.data {
		if card.EncryptedPAN.KeyVersion == keyVersion && card.Status != model.CardStatusDeleted {
			result = append(result, clone(card))
		}
	}
	return result, nil
}
