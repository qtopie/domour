package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	skillpkg "github.com/qtopie/domour/internal/bionic/skill"
	"github.com/qtopie/domour/internal/config"
)

type SkillInfo struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description,omitempty"`
	SourcePath  string                    `json:"source_path,omitempty"`
	Provider    string                    `json:"provider,omitempty"`
	Format      string                    `json:"format,omitempty"`
	Loaded      bool                      `json:"loaded"`
	Tools       []skillpkg.ToolDefinition `json:"tools,omitempty"`
}

type SkillSnapshot struct {
	Name         string                    `json:"name"`
	Description  string                    `json:"description,omitempty"`
	Instructions string                    `json:"instructions,omitempty"`
	SourcePath   string                    `json:"source_path,omitempty"`
	Provider     string                    `json:"provider,omitempty"`
	Format       string                    `json:"format,omitempty"`
	Tools        []skillpkg.ToolDefinition `json:"tools,omitempty"`
}

type SkillLoader func(ctx context.Context) (*skillpkg.Skill, error)

type SkillSpec struct {
	Name        string
	Description string
	SourcePath  string
	Provider    string
	Format      string
	IdleTTL     time.Duration
	Load        SkillLoader
}

type skillState struct {
	spec     SkillSpec
	loaded   *skillpkg.Skill
	lastUsed time.Time
	loading  bool
	cond     *sync.Cond
}

func (m *Manager) RegisterSkill(spec SkillSpec) error {
	if err := validateSkillSpec(spec); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.skills[spec.Name]; exists {
		return fmt.Errorf("skill %q already registered", spec.Name)
	}
	state := &skillState{spec: normalizeSkillSpec(spec)}
	state.cond = sync.NewCond(&m.mu)
	m.skills[state.spec.Name] = state
	return nil
}

func (m *Manager) LoadDefaultSkillSources() error {
	for _, source := range discoverDefaultSkillSources() {
		if err := m.RegisterSkill(source); err != nil && !strings.Contains(err.Error(), "already registered") {
			return err
		}
	}

	cfg, err := config.LoadDomourConfig()
	if err == nil {
		skillsDir := strings.TrimSpace(cfg.SkillsDir)
		if skillsDir != "" {
			if err := m.LoadSkillsFromDir(skillsDir); err != nil {
				slog.Warn("Failed to load skills from configured directory", "dir", skillsDir, "error", err)
			}
		}
	}

	return nil
}

func (m *Manager) LoadSkillsFromDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}

	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("skills path %s is not a directory", dir)
	}

	for _, source := range discoverMarkdownFiles(dir, "domour", "skill-md", "", false) {
		if err := m.RegisterSkill(source); err != nil && !strings.Contains(err.Error(), "already registered") {
			return err
		}
	}

	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			spec := NewJSONFileSkill(path)
			spec.Name = buildProviderSkillName("domour", path)
			if err := m.RegisterSkill(spec); err != nil && !strings.Contains(err.Error(), "already registered") {
				slog.Warn("Failed to register JSON skill", "path", path, "error", err)
			}
		}
	}

	return nil
}

func (m *Manager) ListSkills() []SkillInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	skills := make([]SkillInfo, 0, len(m.skills))
	for _, state := range m.skills {
		info := SkillInfo{
			Name:        state.spec.Name,
			Description: state.spec.Description,
			SourcePath:  state.spec.SourcePath,
			Provider:    state.spec.Provider,
			Format:      state.spec.Format,
			Loaded:      state.loaded != nil,
		}
		if state.loaded != nil {
			info.Tools = cloneToolDefinitions(state.loaded.Tools)
		}
		skills = append(skills, info)
	}
	return skills
}

func (m *Manager) ResolveSkill(ctx context.Context, name string) (SkillSnapshot, error) {
	m.mu.Lock()
	state, ok := m.skills[strings.TrimSpace(name)]
	if !ok {
		m.mu.Unlock()
		return SkillSnapshot{}, fmt.Errorf("skill %q is not registered", name)
	}
	for state.loading {
		state.cond.Wait()
	}
	if state.loaded == nil {
		load := state.spec.Load
		state.loading = true
		m.mu.Unlock()
		loaded, err := load(ctx)
		m.mu.Lock()
		state.loading = false
		state.cond.Broadcast()
		if err != nil {
			m.mu.Unlock()
			return SkillSnapshot{}, err
		}
		state.loaded = loaded
	}

	state.lastUsed = time.Now()
	loaded := state.loaded
	snapshot := SkillSnapshot{
		Name:         firstNonEmpty(strings.TrimSpace(loaded.Name), state.spec.Name),
		Description:  firstNonEmpty(strings.TrimSpace(loaded.Description), state.spec.Description),
		Instructions: strings.TrimSpace(loaded.Instructions),
		SourcePath:   state.spec.SourcePath,
		Provider:     state.spec.Provider,
		Format:       state.spec.Format,
		Tools:        cloneToolDefinitions(loaded.Tools),
	}
	m.mu.Unlock()
	return snapshot, nil
}

func (m *Manager) BuildSkillInstruction(ctx context.Context, name string) (string, error) {
	snapshot, err := m.ResolveSkill(ctx, name)
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	builder.WriteString("Skill: ")
	builder.WriteString(snapshot.Name)
	if snapshot.Description != "" {
		builder.WriteString("\nDescription: ")
		builder.WriteString(snapshot.Description)
	}
	if snapshot.Instructions != "" {
		builder.WriteString("\nInstructions:\n")
		builder.WriteString(snapshot.Instructions)
	}
	if len(snapshot.Tools) > 0 {
		builder.WriteString("\nAllowed tools:\n")
		for _, tool := range snapshot.Tools {
			builder.WriteString("- ")
			builder.WriteString(tool.Name)
			if tool.Description != "" {
				builder.WriteString(": ")
				builder.WriteString(tool.Description)
			}
			builder.WriteString("\n")
		}
	}
	return strings.TrimSpace(builder.String()), nil
}

func (m *Manager) UnloadSkill(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.skills[strings.TrimSpace(name)]
	if !ok {
		return fmt.Errorf("skill %q is not registered", name)
	}
	state.loaded = nil
	state.lastUsed = time.Time{}
	return nil
}

func (m *Manager) UnloadIdleSkills() {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, state := range m.skills {
		if state.loaded == nil {
			continue
		}
		if now.Sub(state.lastUsed) < state.spec.IdleTTL {
			continue
		}
		state.loaded = nil
		state.lastUsed = time.Time{}
	}
}

func validateSkillSpec(spec SkillSpec) error {
	if strings.TrimSpace(spec.Name) == "" {
		return fmt.Errorf("skill name is required")
	}
	if spec.Load == nil {
		return fmt.Errorf("skill %q loader is required", spec.Name)
	}
	return nil
}

func normalizeSkillSpec(spec SkillSpec) SkillSpec {
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Description = strings.TrimSpace(spec.Description)
	spec.SourcePath = strings.TrimSpace(spec.SourcePath)
	spec.Provider = strings.TrimSpace(spec.Provider)
	spec.Format = strings.TrimSpace(spec.Format)
	if spec.IdleTTL <= 0 {
		spec.IdleTTL = defaultIdleTTL
	}
	return spec
}

func NewFileSkill(path string) SkillSpec {
	path = strings.TrimSpace(path)
	baseName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return SkillSpec{
		Name:       strings.ToLower(baseName),
		SourcePath: path,
		Provider:   "domour",
		Format:     "skill-md",
		Load: func(ctx context.Context) (*skillpkg.Skill, error) {
			_ = ctx
			loaded, err := skillpkg.ParseSkill(path)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(loaded.Name) == "" {
				loaded.Name = strings.ToLower(baseName)
			}
			return loaded, nil
		},
	}
}

func NewJSONFileSkill(path string) SkillSpec {
	path = strings.TrimSpace(path)
	baseName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return SkillSpec{
		Name:       strings.ToLower(baseName),
		SourcePath: path,
		Provider:   "domour",
		Format:     "skill-json",
		Load: func(ctx context.Context) (*skillpkg.Skill, error) {
			_ = ctx
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			var loaded skillpkg.Skill
			if err := json.Unmarshal(data, &loaded); err != nil {
				return nil, err
			}
			if strings.TrimSpace(loaded.Name) == "" {
				loaded.Name = strings.ToLower(baseName)
			}
			return &loaded, nil
		},
	}
}

func NewInstructionSkill(path, provider, format, name string) SkillSpec {
	path = strings.TrimSpace(path)
	provider = strings.TrimSpace(provider)
	format = firstNonEmpty(strings.TrimSpace(format), "instruction-md")
	name = firstNonEmpty(strings.TrimSpace(name), buildProviderSkillName(provider, path))

	return SkillSpec{
		Name:        name,
		Description: fmt.Sprintf("%s instructions from %s", strings.Title(provider), filepath.Base(path)),
		SourcePath:  path,
		Provider:    provider,
		Format:      format,
		Load: func(ctx context.Context) (*skillpkg.Skill, error) {
			_ = ctx
			content, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			return &skillpkg.Skill{
				Name:         humanizeSkillName(name),
				Description:  fmt.Sprintf("%s instructions", strings.Title(provider)),
				Instructions: strings.TrimSpace(string(content)),
			}, nil
		},
	}
}

func cloneToolDefinitions(input []skillpkg.ToolDefinition) []skillpkg.ToolDefinition {
	if len(input) == 0 {
		return nil
	}
	cloned := make([]skillpkg.ToolDefinition, len(input))
	copy(cloned, input)
	return cloned
}

func defaultSkillsDir() string {
	if value := strings.TrimSpace(os.Getenv("DOMOUR_SKILLS_DIR")); value != "" {
		return value
	}
	return "skills"
}

func discoverDefaultSkillSources() []SkillSpec {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()

	var specs []SkillSpec
	specs = append(specs, discoverMarkdownFiles(defaultSkillsDir(), "domour", "skill-md", "", false)...)

	if cwd != "" {
		specs = append(specs, discoverMarkdownFiles(cwd, "gemini", "instruction-md", "GEMINI.md", false)...)
		specs = append(specs, discoverMarkdownFiles(cwd, "claude-code", "instruction-md", "CLAUDE.md", false)...)
		specs = append(specs, discoverMarkdownFiles(filepath.Join(cwd, ".claude"), "claude-code", "instruction-md", "CLAUDE.md", true)...)
		specs = append(specs, discoverMarkdownFiles(cwd, "claude-code", "instruction-md", "CLAUDE.local.md", false)...)
		specs = append(specs, discoverMarkdownFiles(filepath.Join(cwd, ".claude", "rules"), "claude-code", "instruction-md", "", true)...)
		specs = append(specs, discoverMarkdownFiles(cwd, "github-copilot", "instruction-md", "AGENTS.md", false)...)
		specs = append(specs, discoverMarkdownFiles(filepath.Join(cwd, ".github"), "github-copilot", "instruction-md", "copilot-instructions.md", false)...)
		specs = append(specs, discoverInstructionTree(filepath.Join(cwd, ".github", "instructions"), "github-copilot", "instruction-md", ".instructions.md")...)
		specs = append(specs, discoverMarkdownFiles(cwd, "qoder", "instruction-md", "QODER.md", false)...)
		specs = append(specs, discoverMarkdownFiles(filepath.Join(cwd, ".qoder"), "qoder", "instruction-md", "", true)...)
	}
	if home != "" {
		specs = append(specs, discoverMarkdownFiles(filepath.Join(home, ".gemini"), "gemini", "instruction-md", "GEMINI.md", false)...)
		specs = append(specs, discoverMarkdownFiles(filepath.Join(home, ".claude"), "claude-code", "instruction-md", "CLAUDE.md", false)...)
		specs = append(specs, discoverMarkdownFiles(filepath.Join(home, ".copilot"), "github-copilot", "instruction-md", "copilot-instructions.md", false)...)
		specs = append(specs, discoverMarkdownFiles(filepath.Join(home, ".qoder"), "qoder", "instruction-md", "", true)...)
	}

	return dedupeSkillSpecs(specs)
}

func discoverMarkdownFiles(root, provider, format, exactFile string, recursive bool) []SkillSpec {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}

	if exactFile != "" {
		match := filepath.Join(root, exactFile)
		fileInfo, err := os.Stat(match)
		if err == nil && !fileInfo.IsDir() {
			return buildSkillSpecsFromMatches([]string{match}, provider, format)
		}
		return nil
	}

	var matches []string
	if !recursive {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
				continue
			}
			matches = append(matches, filepath.Join(root, entry.Name()))
		}
		return buildSkillSpecsFromMatches(matches, provider, format)
	}

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			matches = append(matches, path)
		}
		return nil
	})
	return buildSkillSpecsFromMatches(matches, provider, format)
}

func discoverInstructionTree(root, provider, format, suffix string) []SkillSpec {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}

	var matches []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), strings.ToLower(suffix)) {
			matches = append(matches, path)
		}
		return nil
	})
	return buildSkillSpecsFromMatches(matches, provider, format)
}

func buildSkillSpecsFromMatches(matches []string, provider, format string) []SkillSpec {
	var specs []SkillSpec
	seen := make(map[string]struct{})
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil || info.IsDir() {
			continue
		}
		if _, ok := seen[match]; ok {
			continue
		}
		seen[match] = struct{}{}
		if provider == "domour" && format == "skill-md" {
			spec := NewFileSkill(match)
			spec.Name = buildProviderSkillName(provider, match)
			spec.Provider = provider
			spec.Format = format
			spec.SourcePath = match
			specs = append(specs, spec)
			continue
		}
		specs = append(specs, NewInstructionSkill(match, provider, format, buildProviderSkillName(provider, match)))
	}
	return specs
}

func dedupeSkillSpecs(specs []SkillSpec) []SkillSpec {
	seen := make(map[string]struct{})
	result := make([]SkillSpec, 0, len(specs))
	for _, spec := range specs {
		key := spec.Provider + "|" + spec.SourcePath
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, spec)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Provider == result[j].Provider {
			return result[i].Name < result[j].Name
		}
		return result[i].Provider < result[j].Provider
	})
	return result
}

func buildProviderSkillName(provider, path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return strings.Join([]string{normalizeNamePart(provider), normalizeNamePart(base)}, ":")
}

func normalizeNamePart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "-", "_", "-", ".", "-", "/", "-", "\\", "-")
	value = replacer.Replace(value)
	value = strings.Trim(value, "-")
	if value == "" {
		return "default"
	}
	return value
}

func humanizeSkillName(value string) string {
	parts := strings.Split(value, ":")
	for i, part := range parts {
		parts[i] = strings.Title(strings.ReplaceAll(part, "-", " "))
	}
	return strings.Join(parts, " / ")
}
