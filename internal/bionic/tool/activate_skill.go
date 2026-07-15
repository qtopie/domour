package tool

import (
	"context"
	"fmt"

	"github.com/qtopie/domour/internal/bionic/skillmgr"
)

// NewActivateSkillTool creates a tool that lets the AI activate a skill by name.
// The tool resolves the skill as a side effect and stores the instructions in the
// SkillManager for the orchestrator to inject into the system prompt. The tool
// returns only a minimal confirmation — the full instructions are NOT returned as
// the tool observation to avoid duplication in the conversation history.
func (m *Manager) NewActivateSkillTool() ToolSpec {
	return NewInternalTool("activate_skill", "Activate a registered skill by name. The skill's instructions and tools will be made available after activation.", func(ctx context.Context, command Command) (Result, error) {
		name, _ := command.Input["skill_name"].(string)
		if name == "" {
			return Result{}, fmt.Errorf("skill_name is required")
		}
		instructions, err := m.SkillMgr.BuildActiveSkillPrompt(ctx, name)
		if err != nil {
			return Result{}, err
		}
		m.SkillMgr.SetActiveSkillPrompt(instructions)
		return Result{
			Observation: "✓ Skill activated. Its tools and instructions are now available.",
			Done:        true,
			Meta: map[string]string{
				"skill": name,
			},
		}, nil
	})
}

// ListSkills delegates to SkillMgr.
func (m *Manager) ListSkills() []skillmgr.SkillInfo {
	return m.SkillMgr.ListSkills()
}

// ResolveSkill delegates to SkillMgr.
func (m *Manager) ResolveSkill(ctx context.Context, name string) (skillmgr.SkillSnapshot, error) {
	return m.SkillMgr.ResolveSkill(ctx, name)
}

// DetectActiveSkill delegates to SkillMgr.
func (m *Manager) DetectActiveSkill(ctx context.Context, query string) string {
	return m.SkillMgr.DetectActiveSkill(ctx, query)
}

// BuildActiveSkillPrompt delegates to SkillMgr.
func (m *Manager) BuildActiveSkillPrompt(ctx context.Context, name string) (string, error) {
	return m.SkillMgr.BuildActiveSkillPrompt(ctx, name)
}

// BuildAvailableSkillsPrompt delegates to SkillMgr.
func (m *Manager) BuildAvailableSkillsPrompt(ctx context.Context) (string, error) {
	return m.SkillMgr.BuildAvailableSkillsPrompt(ctx)
}

// ActiveSkillPrompt delegates to SkillMgr.
func (m *Manager) ActiveSkillPrompt() string {
	return m.SkillMgr.ActiveSkillPrompt()
}

// SetActiveSkillPrompt delegates to SkillMgr.
func (m *Manager) SetActiveSkillPrompt(prompt string) {
	m.SkillMgr.SetActiveSkillPrompt(prompt)
}

// ClearActiveSkillPrompt delegates to SkillMgr.
func (m *Manager) ClearActiveSkillPrompt() {
	m.SkillMgr.ClearActiveSkillPrompt()
}
