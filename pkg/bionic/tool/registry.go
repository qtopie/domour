package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Tool represents a custom tool definition that can be registered by other modules.
type Tool struct {
	Name        string
	Description string
	Parameters  string // Optional JSON parameters schema (raw JSON string)
	Act         func(ctx context.Context, input map[string]interface{}) (string, error)
}

var (
	mu    sync.Mutex
	tools []Tool
)

// Register registers a public tool.
func Register(t Tool) {
	mu.Lock()
	defer mu.Unlock()
	tools = append(tools, t)
}

// List returns all registered public tools.
func List() []Tool {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Tool, len(tools))
	copy(out, tools)
	return out
}

// toolDef is a JSON-deserializable tool definition for loading from files.
// The Act field is omitted — tools loaded from disk have no Go function
// attached; they serve as definitions that tools registry can discover
// (e.g., for proxy-based execution).
type toolDef struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  string `json:"parameters,omitempty"`
}

// LoadFromDir scans a directory for .json tool definition files (non-recursive),
// parses each, and registers them via Register. Tools loaded from disk have no
// Act function — they serve as discoverable definitions only.
func LoadFromDir(dir string) ([]Tool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read tools dir %s: %w", dir, err)
	}

	var loaded []Tool
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return loaded, fmt.Errorf("read tool file %s: %w", path, err)
		}
		var def toolDef
		if err := json.Unmarshal(data, &def); err != nil {
			return loaded, fmt.Errorf("parse tool %s: %w", path, err)
		}
		if def.Name == "" {
			def.Name = strings.TrimSuffix(entry.Name(), ".json")
		}
		t := Tool{
			Name:        def.Name,
			Description: def.Description,
			Parameters:  def.Parameters,
		}
		Register(t)
		loaded = append(loaded, t)
	}
	return loaded, nil
}

// LoadFromDirs loads tools from multiple directories, merging and deduplicating
// by tool name. Directories are processed in order — when the same tool name
// appears in multiple directories, the last occurrence wins (higher priority).
// It returns the merged list and any errors encountered.
func LoadFromDirs(dirs ...string) ([]Tool, error) {
	seen := make(map[string]int) // tool name -> index in merged
	var merged []Tool
	var errs []error

	for _, dir := range dirs {
		loaded, err := LoadFromDir(dir)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", dir, err))
			continue
		}
		for _, t := range loaded {
			if idx, ok := seen[t.Name]; ok {
				merged[idx] = t // override: later wins
			} else {
				seen[t.Name] = len(merged)
				merged = append(merged, t)
			}
		}
	}

	if len(errs) > 0 {
		return merged, fmt.Errorf("errors loading tools: %v", errs)
	}
	return merged, nil
}
