// Package memory provides the global shared memory/knowledge layer (MemoryContextManager).
//
// This package manages Tier 1–3 context sources:
//   - Tier 1: System instructions, global rules (~/.domour/*.md, skills/tools metadata)
//   - Tier 2: Project memory (<project>/.domour/*.md)
//   - Tier 3: JIT file discovery (on-demand, workspace-relative file reading)
//
// It is designed as a shared singleton across all Agent instances — "one session/project,
// one MemoryContextManager". L1 caching via Otter ensures hot paths stay in memory.
//
// Analogy: cerebral cortex, public library, rule book.
package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/qtopie/domour/internal/bionic/tool"
)

// MemoryContextManager is the global shared memory/knowledge layer.
// All Agent instances share one instance to ensure consistent "world view".
//
// Cache design (see context_cache.go for details):
//   - Tier 1 (global rules) → key: tier1:global — shared across ALL Agents
//   - Tier 2 (project mem)  → key: tier2:project:<hash> — shared by project dir
//   - Tier 3 (JIT files)    → key: tier3:file:<hash> — per-file, short TTL
//   - ctx:assemble/render   → key: ctx:*:<session>:<agent>:<seq> — per-Agent
type MemoryContextManager struct {
	globalDir    string        // ~/.domour/
	projectDir   string        // <project>/.domour/
	cache        *ContextCache // Shared L1 cache (all agents share this)
	mu           sync.RWMutex
	lastLoadTime time.Time
}

// MemoryContextConfig carries optional overrides for the memory manager.
type MemoryContextConfig struct {
	GlobalDir  string        // Override ~/.domour/ path
	ProjectDir string        // Override project .domour/ path
	CacheSize  int           // L1 cache capacity for ContextCache
}

// NewMemoryContextManager creates the shared memory context manager.
// The single ContextCache is shared across all Agent instances — Tier 1 and
// Tier 2 entries are keyed by content hash, so every Agent sees the same
// global context from the same cache line.
func NewMemoryContextManager(cfg *MemoryContextConfig) *MemoryContextManager {
	if cfg == nil {
		cfg = &MemoryContextConfig{}
	}
	cacheSize := cfg.CacheSize
	if cacheSize <= 0 {
		cacheSize = 2048
	}

	return &MemoryContextManager{
		globalDir:  resolveGlobalDir(cfg.GlobalDir),
		projectDir: cfg.ProjectDir,
		cache:      NewContextCache(cacheSize),
	}
}

// resolveGlobalDir finds the global Domour config directory.
func resolveGlobalDir(override string) string {
	if override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".domour")
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir
	}
	return ""
}

// SetProjectDir sets the project-level .domour directory.
func (m *MemoryContextManager) SetProjectDir(projectDir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.projectDir = strings.TrimSpace(projectDir)
	m.lastLoadTime = time.Time{} // force reload
}

// ---------------------------------------------------------------------------
// Tier 1: System Instruction
// ---------------------------------------------------------------------------

// LoadSystemPrompt builds the Tier 1 system instruction block.
// Sources: global rules + skills/tools metadata + optional interception note.
//
// The interceptionNote is expected to be pre-built by the caller (e.g.,
// via BuildInterceptionSystemNote from the context package), to avoid
// circular dependency between memory and context packages.
func (m *MemoryContextManager) LoadSystemPrompt(ctx context.Context, toolMgr *tool.Manager, userMessage string, attachments []AttachmentHandler, interceptionNote string) string {
	var parts []string

	// 1a. Base identity
	parts = append(parts, m.loadGlobalMemoryFiles())

	// 1b. Skills & tools metadata
	if toolMgr != nil {
		if matched := toolMgr.DetectActiveSkill(ctx, userMessage); matched != "" {
			if activePrompt, err := toolMgr.BuildActiveSkillPrompt(ctx, matched); err == nil && activePrompt != "" {
				parts = append(parts, activePrompt)
			}
		} else {
			if availablePrompt, err := toolMgr.BuildAvailableSkillsPrompt(ctx); err == nil && availablePrompt != "" {
				parts = append(parts, availablePrompt)
			}
		}
	}

	// 1c. Interception note (OCR evidence etc.)
	if interceptionNote != "" {
		parts = append(parts, interceptionNote)
	}

	return strings.Join(parts, "\n\n")
}

// loadGlobalMemoryFiles reads all *.md from ~/.domour/ and sorts by filename.
// Cached in L1 at key "tier1:global" — shared across ALL Agent instances.
func (m *MemoryContextManager) loadGlobalMemoryFiles() string {
	if m.globalDir == "" {
		return ""
	}

	// Fast path: L1 cache hit (shared across all agents)
	if m.cache != nil {
		if cached, ok := m.cache.GetTier1Global(); ok {
			return cached
		}
	}

	files, err := filepath.Glob(filepath.Join(m.globalDir, "*.md"))
	if err != nil || len(files) == 0 {
		return ""
	}

	sort.Strings(files)
	var parts []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content != "" {
			name := strings.TrimSuffix(filepath.Base(f), ".md")
			parts = append(parts, fmt.Sprintf("## %s\n%s", name, content))
		}
	}

	result := strings.Join(parts, "\n\n")
	if m.cache != nil {
		m.cache.SetTier1Global(result)
	}
	return result
}

// ---------------------------------------------------------------------------
// Tier 2: Project Context
// ---------------------------------------------------------------------------

// LoadProjectContext builds the Tier 2 project memory block.
// Sources: .domour/*.md in the project directory.
// Cached at key "tier2:project:<hash>" — shared across all Agents in the same project.
func (m *MemoryContextManager) LoadProjectContext() string {
	m.mu.RLock()
	projectDir := m.projectDir
	m.mu.RUnlock()
	if projectDir == "" {
		return ""
	}

	// Fast path: L1 cache hit (shared by project dir)
	if m.cache != nil {
		if cached, ok := m.cache.GetTier2Project(projectDir); ok {
			return cached
		}
	}

	domourDir := filepath.Join(projectDir, ".domour")
	files, err := filepath.Glob(filepath.Join(domourDir, "*.md"))
	if err != nil || len(files) == 0 {
		return ""
	}

	sort.Strings(files)
	var parts []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content != "" {
			name := strings.TrimSuffix(filepath.Base(f), ".md")
			parts = append(parts, fmt.Sprintf("# Project: %s\n%s", name, content))
		}
	}

	result := strings.Join(parts, "\n\n")
	if m.cache != nil {
		m.cache.SetTier2Project(projectDir, result)
	}
	return result
}

// ---------------------------------------------------------------------------
// Tier 3: JIT File Discovery
// ---------------------------------------------------------------------------

// JITDiscoverContext discovers files relative to the workspace at access time.
// Each file is cached individually at key "tier3:file:<hash>" — shared by path.
// Invalidation: files expire automatically after Tier3Policy.TTL (5min).
func (m *MemoryContextManager) JITDiscoverContext(ctx context.Context, workspace string, paths []string) string {
	if workspace == "" || len(paths) == 0 {
		return ""
	}

	var parts []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		fullPath := p
		if !filepath.IsAbs(p) {
			fullPath = filepath.Join(workspace, p)
		}

		// Per-file L1 cache lookup
		var content string
		if m.cache != nil {
			if cached, ok := m.cache.GetTier3File(fullPath); ok {
				content = cached
			}
		}

		if content == "" {
			data, err := os.ReadFile(fullPath)
			if err != nil {
				continue
			}
			content = strings.TrimSpace(string(data))
			if content != "" && m.cache != nil {
				m.cache.SetTier3File(fullPath, content)
			}
		}

		if content != "" {
			relPath := p
			if abs, err := filepath.Rel(workspace, fullPath); err == nil {
				relPath = abs
			}
			parts = append(parts, fmt.Sprintf("--- %s ---\n%s", relPath, content))
		}
	}
	return strings.Join(parts, "\n\n")
}

// ---------------------------------------------------------------------------
// LTM: Long-Term Memory Interface (key-value)
// ---------------------------------------------------------------------------

// Recall retrieves raw data from long-term memory by query.
// Returns nil if not found (no-op default implementation).
func (m *MemoryContextManager) Recall(ctx context.Context, query string) ([]byte, error) {
	// TODO: Implement with Dapr Vector Store / Chroma / SurrealDB
	return nil, nil
}

// Memorize stores raw data into long-term memory by key.
func (m *MemoryContextManager) Memorize(ctx context.Context, key string, data []byte) error {
	// TODO: Implement with Dapr State Store
	return nil
}

// Cache returns the underlying ContextCache for direct access.
// ContextManager uses this to write tool results to the shared session cache.
func (m *MemoryContextManager) Cache() *ContextCache {
	return m.cache
}

// InvalidateCache clears cache entries matching the given tier pattern.
// Supported patterns: "tier1", "tier2", "tier3", "all"
func (m *MemoryContextManager) InvalidateCache(pattern string) {
	switch pattern {
	case "tier1":
		if m.cache != nil {
			m.cache.InvalidateTier1Global()
		}
	case "tier2":
		m.mu.RLock()
		dir := m.projectDir
		m.mu.RUnlock()
		if dir != "" && m.cache != nil {
			m.cache.InvalidateTier2Project(dir)
		}
	case "tier3":
		// Per-file cache keys auto-expire via TTL.
		// Add selective invalidation per-path if needed.
	default:
		// "all" — best-effort: clear cache, reset stats
		if m.cache != nil {
			m.cache.ResetStats()
		}
	}
	m.mu.Lock()
	m.lastLoadTime = time.Time{}
	m.mu.Unlock()
}

// ---------------------------------------------------------------------------
// AttachmentHandler interface
// ---------------------------------------------------------------------------

// AttachmentHandler is satisfied by types that provide MIME type info
// (e.g., shared.BrainAttachment). The interface lives here so that
// MemoryContextManager can accept attachments without importing the
// shared package directly.
type AttachmentHandler interface {
	GetMIMEType() string
}
