// Package event 提供跨上下文共享的领域事件基础类型。
// 各上下文的具体事件结构体继续保留在各自 domain/event 包中。
package event

// DomainEvent 领域事件标记接口，被所有上下文的事件结构体实现。
type DomainEvent interface {
	EventName() string
}
