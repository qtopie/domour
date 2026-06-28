package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type FileRegistry struct {
	dir string
	mu  sync.RWMutex
}

func NewFileRegistry(dir string) *FileRegistry {
	return &FileRegistry{
		dir: dir,
	}
}

func (r *FileRegistry) Register(ctx context.Context, s *Skill) error {
	_ = ctx
	if s == nil || s.ID == "" {
		return fmt.Errorf("invalid skill specification: ID is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := os.MkdirAll(r.dir, 0755); err != nil {
		return fmt.Errorf("create registry directory: %w", err)
	}

	path := filepath.Join(r.dir, s.ID+".json")
	
	now := time.Now().Unix()
	s.UpdatedAt = now
	if s.CreatedAt == 0 {
		s.CreatedAt = now
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize skill: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write skill file: %w", err)
	}

	return nil
}

func (r *FileRegistry) Get(ctx context.Context, id string) (*Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Try reading JSON manifest first
	jsonPath := filepath.Join(r.dir, id+".json")
	if data, err := os.ReadFile(jsonPath); err == nil {
		var s Skill
		if err := json.Unmarshal(data, &s); err == nil {
			return &s, nil
		}
	}

	// Try reading Markdown file
	mdPath := filepath.Join(r.dir, id+".md")
	if _, err := os.Stat(mdPath); err == nil {
		s, err := ParseSkill(mdPath)
		if err != nil {
			return nil, fmt.Errorf("parse markdown skill %s: %w", id, err)
		}
		s.ID = id
		return s, nil
	}

	return nil, fmt.Errorf("skill %s not found in file registry %s", id, r.dir)
}

func (r *FileRegistry) List(ctx context.Context) ([]*Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, err := os.Stat(r.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read directory info: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("file registry path %s is not a directory", r.dir)
	}

	var list []*Skill
	err = filepath.WalkDir(r.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		base := filepath.Base(path)
		id := strings.TrimSuffix(base, filepath.Ext(base))

		if ext == ".json" {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			var s Skill
			if err := json.Unmarshal(data, &s); err == nil {
				if s.ID == "" {
					s.ID = id
				}
				list = append(list, &s)
			}
		} else if ext == ".md" {
			s, err := ParseSkill(path)
			if err == nil {
				s.ID = id
				list = append(list, s)
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walk file registry: %w", err)
	}

	return list, nil
}

func (r *FileRegistry) Delete(ctx context.Context, id string) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()

	jsonPath := filepath.Join(r.dir, id+".json")
	mdPath := filepath.Join(r.dir, id+".md")

	jsonRemoved := os.Remove(jsonPath) == nil
	mdRemoved := os.Remove(mdPath) == nil

	if !jsonRemoved && !mdRemoved {
		return fmt.Errorf("skill %s not found to delete in file registry", id)
	}

	return nil
}
