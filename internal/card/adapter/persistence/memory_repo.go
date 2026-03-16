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

// 编译期检查接口实现
var _ port.CardRepository = (*InMemoryCardRepository)(nil)

func NewInMemoryCardRepository() *InMemoryCardRepository {
	return &InMemoryCardRepository{
		data: make(map[model.SavedCardID]*model.SavedCard),
	}
}

// clone 对聚合根做浅拷贝，隔离仓储内部数据与外部调用方
// Events 字段由仓储自身不关心，置 nil 避免共享切片底层数组
func clone(card *model.SavedCard) *model.SavedCard {
	c := *card      // 值拷贝（浅拷贝结构体所有字段）
	c.Events = nil  // 仓储不持有未发布事件
	return &c
}

// Save 新增或更新（upsert）。存储副本，隔离外部修改。
func (r *InMemoryCardRepository) Save(_ context.Context, card *model.SavedCard) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[card.ID] = clone(card)
	return nil
}

// FindByID 按卡 ID 查询，返回副本防止调用方修改仓储数据
func (r *InMemoryCardRepository) FindByID(_ context.Context, id model.SavedCardID) (*model.SavedCard, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	card, ok := r.data[id]
	if !ok {
		return nil, model.ErrCardNotFound
	}
	c := *card
	return &c, nil
}

// FindAllByUserID 查询用户全部未删除的卡，每项返回副本
func (r *InMemoryCardRepository) FindAllByUserID(_ context.Context, userID string) ([]*model.SavedCard, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*model.SavedCard
	for _, card := range r.data {
		if card.UserID == userID && card.Status != model.CardStatusDeleted {
			c := *card
			result = append(result, &c)
		}
	}
	return result, nil
}

// FindDefaultByUserID 查询用户当前默认卡，无则返回 nil, nil；返回副本
func (r *InMemoryCardRepository) FindDefaultByUserID(_ context.Context, userID string) (*model.SavedCard, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, card := range r.data {
		if card.UserID == userID && card.IsDefault && card.Status != model.CardStatusDeleted {
			c := *card
			return &c, nil
		}
	}
	return nil, nil
}
