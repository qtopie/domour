package discovery

import (
	"sync"
	"time"
)

type ResourceType string

const (
	ResourceLLM   ResourceType = "llm"
	ResourceAgent ResourceType = "agent"
)

type Capability string

const (
	CapOCR    Capability = "ocr"
	CapVision Capability = "vision"
	CapTools  Capability = "tools"
	CapWeb    Capability = "web"
)

type ResourceStatus string

const (
	StatusOnline  ResourceStatus = "online"
	StatusOffline ResourceStatus = "offline"
	StatusUnknown ResourceStatus = "unknown"
)

type Entry struct {
	ID           string            `json:"id"`
	Type         ResourceType      `json:"type"`
	Provider     string            `json:"provider"`
	Name         string            `json:"name"`
	Capabilities []Capability      `json:"capabilities"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Source       string            `json:"source,omitempty"`
	Status       ResourceStatus    `json:"status"`
	LastCheck    int64             `json:"last_check"` // Unix timestamp
}

type Registry struct {
	mu      sync.RWMutex
	entries map[string]Entry
}

func New() *Registry {
	return &Registry{
		entries: make(map[string]Entry),
	}
}

func (r *Registry) Register(entry Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry.Status == "" {
		entry.Status = StatusUnknown
	}
	r.entries[entry.ID] = entry
}

func (r *Registry) SetStatus(id string, status ResourceStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry, ok := r.entries[id]; ok {
		entry.Status = status
		entry.LastCheck = time.Now().Unix()
		r.entries[id] = entry
	}
}

func (r *Registry) TouchStatus(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry, ok := r.entries[id]; ok {
		entry.Status = StatusOnline
		entry.LastCheck = time.Now().Unix()
		r.entries[id] = entry
	}
}

func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, id)
}

func (r *Registry) List(filter func(Entry) bool) []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Entry
	for _, entry := range r.entries {
		if filter == nil || filter(entry) {
			result = append(result, entry)
		}
	}
	return result
}

func (r *Registry) Get(id string) (Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[id]
	return entry, ok
}

// Global registry for ease of use across the brain
var globalRegistry = New()

func Global() *Registry {
	return globalRegistry
}
