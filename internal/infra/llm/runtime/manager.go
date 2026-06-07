package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type contextKey string

const requestMetadataKey contextKey = "domour.provider.runtime.request"

type RequestMetadata struct {
	SessionID string
	Workspace string
}

type SessionRuntime struct {
	Provider            string
	DomourSessionID     string
	Workspace           string
	RuntimeDir          string
	HomeDir             string
	ConfigDir           string
	ConversationStarted bool
	LastUsedAt          time.Time
	RecoveredWithResume bool
}

type Manager struct {
	rootDir  string
	mu       sync.Mutex
	sessions map[string]*SessionRuntime
}

var (
	defaultManagerOnce sync.Once
	defaultManager     *Manager
)

func WithRequestMetadata(ctx context.Context, metadata RequestMetadata) context.Context {
	return context.WithValue(ctx, requestMetadataKey, metadata)
}

func RequestMetadataFromContext(ctx context.Context) RequestMetadata {
	metadata, _ := ctx.Value(requestMetadataKey).(RequestMetadata)
	return metadata
}

func DefaultManager() *Manager {
	defaultManagerOnce.Do(func() {
		defaultManager = NewManager(defaultRootDir())
	})
	return defaultManager
}

func NewManager(rootDir string) *Manager {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		rootDir = defaultRootDir()
	}
	return &Manager{
		rootDir:  rootDir,
		sessions: make(map[string]*SessionRuntime),
	}
}

func (m *Manager) Prepare(provider, sessionID, workspace string) (*SessionRuntime, error) {
	provider = normalizeProvider(provider)
	if provider == "" {
		return nil, errors.New("provider is required")
	}

	sessionID = normalizeSessionID(sessionID)
	workspace = normalizeWorkspace(workspace)
	key := provider + ":" + sessionID

	m.mu.Lock()
	defer m.mu.Unlock()

	if runtime, ok := m.sessions[key]; ok {
		runtime.LastUsedAt = time.Now()
		if workspace != "" {
			runtime.Workspace = workspace
		}
		return runtime, ensureRuntimeDirs(runtime)
	}

	runtime := &SessionRuntime{
		Provider:        provider,
		DomourSessionID: sessionID,
		Workspace:       workspace,
		RuntimeDir:      filepath.Join(m.rootDir, provider, sanitizeSegment(sessionID)),
		LastUsedAt:      time.Now(),
	}
	runtime.HomeDir = filepath.Join(runtime.RuntimeDir, "home")
	runtime.ConfigDir = filepath.Join(runtime.RuntimeDir, "config")
	if runtime.Workspace == "" {
		runtime.Workspace = filepath.Join(runtime.RuntimeDir, "workspace")
	}

	if err := ensureRuntimeDirs(runtime); err != nil {
		return nil, err
	}

	m.sessions[key] = runtime
	return runtime, nil
}

func (m *Manager) MarkSuccess(runtime *SessionRuntime) {
	if runtime == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	runtime.ConversationStarted = true
	runtime.LastUsedAt = time.Now()
}

func (m *Manager) MarkResume(runtime *SessionRuntime) {
	if runtime == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	runtime.RecoveredWithResume = true
	runtime.LastUsedAt = time.Now()
}

func defaultRootDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(homeDir) == "" {
		return filepath.Join(os.TempDir(), "domour-provider-runtime")
	}
	return filepath.Join(homeDir, ".domour", "provider-runtime")
}

func ensureRuntimeDirs(runtime *SessionRuntime) error {
	paths := []string{
		runtime.RuntimeDir,
		runtime.HomeDir,
		runtime.ConfigDir,
		runtime.Workspace,
		filepath.Join(runtime.HomeDir, ".config"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("failed to create runtime dir %s: %w", path, err)
		}
	}

	realHome, _ := os.UserHomeDir()
	if realHome != "" {
		geminiSrc := filepath.Join(realHome, ".gemini")
		geminiDest := filepath.Join(runtime.HomeDir, ".gemini")
		if _, err := os.Stat(geminiSrc); err == nil {
			os.RemoveAll(geminiDest)
			os.Symlink(geminiSrc, geminiDest)
		}

		configSrc := filepath.Join(realHome, ".config", "github-copilot")
		configDest := filepath.Join(runtime.HomeDir, ".config", "github-copilot")
		if _, err := os.Stat(configSrc); err == nil {
			os.RemoveAll(configDest)
			os.Symlink(configSrc, configDest)
		}
	}
	return nil
}

func normalizeProvider(provider string) string {
	return strings.TrimSpace(strings.ToLower(provider))
}

func normalizeSessionID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "default-session"
	}
	return sessionID
}

func normalizeWorkspace(workspace string) string {
	return strings.TrimSpace(workspace)
}

func sanitizeSegment(value string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		" ", "_",
	)
	value = replacer.Replace(strings.TrimSpace(value))
	if value == "" {
		return "default"
	}
	return value
}
