package store

import (
	"context"
	"sync"

	"github.com/jigframework/jig/tenant"
)

// Memory returns a Store that keeps records in process memory. It is safe for
// concurrent use and loses everything on restart, which makes it right for
// examples, tests and prototypes and wrong for anything else.
func Memory[T any]() Store[T] {
	return &memory[T]{orgs: map[tenant.ID]*bucket[T]{}}
}

type memory[T any] struct {
	mu   sync.RWMutex
	orgs map[tenant.ID]*bucket[T]
}

type bucket[T any] struct {
	order []string // ids in insertion order
	items map[string]T
}

func (m *memory[T]) List(_ context.Context, org tenant.ID) ([]T, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b := m.orgs[org]
	if b == nil {
		return nil, nil
	}
	out := make([]T, 0, len(b.order))
	for _, id := range b.order {
		out = append(out, b.items[id])
	}
	return out, nil
}

func (m *memory[T]) Get(_ context.Context, org tenant.ID, id string) (T, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var zero T
	b := m.orgs[org]
	if b == nil {
		return zero, false, nil
	}
	v, ok := b.items[id]
	return v, ok, nil
}

func (m *memory[T]) Put(_ context.Context, org tenant.ID, id string, v T) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := m.orgs[org]
	if b == nil {
		b = &bucket[T]{items: map[string]T{}}
		m.orgs[org] = b
	}
	if _, exists := b.items[id]; !exists {
		b.order = append(b.order, id)
	}
	b.items[id] = v
	return nil
}

func (m *memory[T]) Delete(_ context.Context, org tenant.ID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := m.orgs[org]
	if b == nil {
		return nil
	}
	if _, exists := b.items[id]; !exists {
		return nil
	}
	delete(b.items, id)
	for i, got := range b.order {
		if got == id {
			b.order = append(b.order[:i:i], b.order[i+1:]...)
			break
		}
	}
	return nil
}
