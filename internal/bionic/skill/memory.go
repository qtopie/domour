package skill

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type MemoryRegistry struct {
	mu     sync.RWMutex
	skills map[string]*Skill
}

func NewMemoryRegistry() *MemoryRegistry {
	return &MemoryRegistry{
		skills: make(map[string]*Skill),
	}
}

func (r *MemoryRegistry) Register(ctx context.Context, s *Skill) error {
	_ = ctx
	if s == nil || s.ID == "" {
		return fmt.Errorf("invalid skill specification: ID is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().Unix()
	existing, ok := r.skills[s.ID]
	if ok {
		s.CreatedAt = existing.CreatedAt
		s.UpdatedAt = now
	} else {
		s.CreatedAt = now
		s.UpdatedAt = now
	}

	r.skills[s.ID] = s
	return nil
}

func (r *MemoryRegistry) Get(ctx context.Context, id string) (*Skill, error) {
	_ = ctx
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, ok := r.skills[id]
	if !ok {
		return nil, fmt.Errorf("skill %s not found in memory registry", id)
	}

	return s, nil
}

func (r *MemoryRegistry) List(ctx context.Context) ([]*Skill, error) {
	_ = ctx
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]*Skill, 0, len(r.skills))
	for _, s := range r.skills {
		list = append(list, s)
	}

	return list, nil
}

func (r *MemoryRegistry) Delete(ctx context.Context, id string) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.skills[id]; !ok {
		return fmt.Errorf("skill %s not found in memory registry", id)
	}

	delete(r.skills, id)
	return nil
}
