package skill

import (
	"context"
	"fmt"
)

type CompositeRegistry struct {
	registries []Registry
}

func NewCompositeRegistry(registries ...Registry) *CompositeRegistry {
	return &CompositeRegistry{
		registries: registries,
	}
}

func (r *CompositeRegistry) Register(ctx context.Context, s *Skill) error {
	if len(r.registries) == 0 {
		return fmt.Errorf("no registries configured in composite registry")
	}

	var firstErr error
	written := false
	for _, reg := range r.registries {
		if err := reg.Register(ctx, s); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		} else {
			written = true
		}
	}

	if !written && firstErr != nil {
		return fmt.Errorf("composite register failed: %w", firstErr)
	}
	return nil
}

func (r *CompositeRegistry) Get(ctx context.Context, id string) (*Skill, error) {
	for _, reg := range r.registries {
		if s, err := reg.Get(ctx, id); err == nil {
			return s, nil
		}
	}
	return nil, fmt.Errorf("skill %s not found in any composite registry backends", id)
}

func (r *CompositeRegistry) List(ctx context.Context) ([]*Skill, error) {
	seen := make(map[string]bool)
	var list []*Skill

	for _, reg := range r.registries {
		skills, err := reg.List(ctx)
		if err != nil {
			continue
		}
		for _, s := range skills {
			if s != nil && !seen[s.ID] {
				seen[s.ID] = true
				list = append(list, s)
			}
		}
	}
	return list, nil
}

func (r *CompositeRegistry) Delete(ctx context.Context, id string) error {
	deleted := false
	var firstErr error
	for _, reg := range r.registries {
		if err := reg.Delete(ctx, id); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		} else {
			deleted = true
		}
	}

	if !deleted && firstErr != nil {
		return fmt.Errorf("composite delete failed: %w", firstErr)
	}
	return nil
}
