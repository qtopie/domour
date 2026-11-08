package skillmgr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	publicskill "github.com/qtopie/domour/ark/skill"
	providerruntime "github.com/qtopie/domour/internal/infra/llm/runtime"
)

func TestManagerLoadsSkillsFromDir(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "todo.md")
	content := "# Todo Skill\n\n" +
		"## Description\n" +
		"Manage TODO items safely.\n\n" +
		"## Instructions\n" +
		"Only use the declared tools.\n\n" +
		"## Tools\n" +
		"```json\n" +
		"[{\"name\":\"todo.list\",\"description\":\"List todos\",\"parameters\":{\"type\":\"object\"}}]\n" +
		"```\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	mgr := NewSkillManager()
	if err := mgr.LoadSkillsFromDir(root); err != nil {
		t.Fatalf("load skills: %v", err)
	}

	list := mgr.ListSkills()
	if len(list) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(list))
	}
	if list[0].Name != "domour:todo" {
		t.Fatalf("unexpected skill name: %s", list[0].Name)
	}

	snapshot, err := mgr.ResolveSkill(context.Background(), "domour:todo")
	if err != nil {
		t.Fatalf("resolve skill: %v", err)
	}
	if snapshot.Name != "Todo Skill" {
		t.Fatalf("unexpected snapshot name: %s", snapshot.Name)
	}
	if len(snapshot.Tools) != 1 || snapshot.Tools[0].Name != "todo.list" {
		t.Fatalf("unexpected skill tools: %+v", snapshot.Tools)
	}
}

func TestManagerBuildsSkillInstructionAndUnloadsIdleSkills(t *testing.T) {
	mgr := NewSkillManager()

	spec := NewFileSkill(writeTempSkill(t))
	spec.Name = "domour:todo"
	spec.IdleTTL = time.Millisecond
	if err := mgr.RegisterSkill(spec); err != nil {
		t.Fatalf("register skill: %v", err)
	}

	text, err := mgr.BuildSkillInstruction(context.Background(), "domour:todo")
	if err != nil {
		t.Fatalf("build skill instruction: %v", err)
	}
	if text == "" {
		t.Fatalf("expected non-empty skill instruction")
	}

	time.Sleep(5 * time.Millisecond)
	mgr.UnloadIdleSkills()

	list := mgr.ListSkills()
	if len(list) != 1 {
		t.Fatalf("expected 1 skill after unload, got %d", len(list))
	}
	if list[0].Loaded {
		t.Fatalf("expected idle skill to be unloaded")
	}
}

func TestInstructionSkillDiscoverySupportsProviderFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "GEMINI.md"), []byte("# Gemini\nUse Go.\n"), 0o600); err != nil {
		t.Fatalf("write gemini file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".github", "instructions", "backend"), 0o755); err != nil {
		t.Fatalf("mkdir copilot instructions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".github", "copilot-instructions.md"), []byte("# Copilot\nPrefer tests.\n"), 0o600); err != nil {
		t.Fatalf("write copilot file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".github", "instructions", "backend", "api.instructions.md"), []byte("# API\nValidate inputs.\n"), 0o600); err != nil {
		t.Fatalf("write nested copilot file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude", "rules"), 0o755); err != nil {
		t.Fatalf("mkdir claude rules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "rules", "testing.md"), []byte("# Testing\nRun go test.\n"), 0o600); err != nil {
		t.Fatalf("write claude rule: %v", err)
	}

	gemini := discoverMarkdownFiles(root, "gemini", "instruction-md", "GEMINI.md", false)
	if len(gemini) != 1 || gemini[0].Name != "gemini:gemini" {
		t.Fatalf("unexpected gemini discovery: %+v", gemini)
	}

	copilot := discoverInstructionTree(filepath.Join(root, ".github", "instructions"), "github-copilot", "instruction-md", ".instructions.md")
	if len(copilot) != 1 || copilot[0].Name != "github-copilot:api-instructions" {
		t.Fatalf("unexpected copilot discovery: %+v", copilot)
	}

	claude := discoverMarkdownFiles(filepath.Join(root, ".claude", "rules"), "claude-code", "instruction-md", "", true)
	if len(claude) != 1 || claude[0].Name != "claude-code:testing" {
		t.Fatalf("unexpected claude discovery: %+v", claude)
	}

	mgr := NewSkillManager()
	for _, spec := range append(append(gemini, copilot...), claude...) {
		if err := mgr.RegisterSkill(spec); err != nil {
			t.Fatalf("register provider skill %s: %v", spec.Name, err)
		}
	}

	snapshot, err := mgr.ResolveSkill(context.Background(), "gemini:gemini")
	if err != nil {
		t.Fatalf("resolve gemini skill: %v", err)
	}
	if snapshot.Provider != "gemini" || snapshot.Instructions == "" {
		t.Fatalf("unexpected gemini snapshot: %+v", snapshot)
	}
}

func writeTempSkill(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	path := filepath.Join(root, "todo.md")
	content := "# Todo Skill\n\n" +
		"## Description\n" +
		"Manage TODO items safely.\n\n" +
		"## Instructions\n" +
		"Only use todo.list when the user asks for a list.\n\n" +
		"## Tools\n" +
		"```json\n" +
		"[{\"name\":\"todo.list\",\"description\":\"List todos\",\"parameters\":{\"type\":\"object\"}}]\n" +
		"```\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp skill: %v", err)
	}
	return path
}

func TestManagerLoadsJSONSkillsFromDir(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "k8s-pod.json")
	content := `{"id":"k8s-pod","name":"k8s-pod","description":"Manage K8s pods","instructions":"Focus on get and restart","tools":[{"name":"k8s.getPods","description":"Get pods"}]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write json skill file: %v", err)
	}

	mgr := NewSkillManager()

	if err := mgr.LoadSkillsFromDir(root); err != nil {
		t.Fatalf("load skills: %v", err)
	}

	list := mgr.ListSkills()
	if len(list) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(list))
	}
	if list[0].Name != "domour:k8s-pod" {
		t.Fatalf("unexpected skill name: %s", list[0].Name)
	}

	snapshot, err := mgr.ResolveSkill(context.Background(), "domour:k8s-pod")
	if err != nil {
		t.Fatalf("resolve skill: %v", err)
	}
	if snapshot.Name != "k8s-pod" || snapshot.Description != "Manage K8s pods" || snapshot.Instructions != "Focus on get and restart" {
		t.Fatalf("unexpected snapshot details: %+v", snapshot)
	}
	if len(snapshot.Tools) != 1 || snapshot.Tools[0].Name != "k8s.getPods" {
		t.Fatalf("unexpected snapshot tools: %+v", snapshot.Tools)
	}
}

func TestManagerLoadsStandardAgentSkills(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "k8s-pod-manager")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}

	content := `---
id: devops.k8s.pod-manager
name: K8s Pod Manager
description: 查看、重启和排查 Pod 状态
intent_tags: ["k8s", "pod", "ops"]
inputs:
  required: ["cluster"]
  optional: ["namespace", "pod_name"]
tools:
  - name: k8s.getPods
    description: Get pods in cluster
    parameters:
      type: object
      properties:
        cluster:
          type: string
---
Focus on get and restart in cluster execution.`

	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write standard SKILL.md file: %v", err)
	}

	// Also write a README.md to ensure it is ignored
	readmePath := filepath.Join(skillDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# README\nSome docs here"), 0o600); err != nil {
		t.Fatalf("write README.md file: %v", err)
	}

	mgr := NewSkillManager()

	if err := mgr.LoadSkillsFromDir(root); err != nil {
		t.Fatalf("load skills: %v", err)
	}

	list := mgr.ListSkills()
	if len(list) != 1 {
		t.Fatalf("expected 1 skill (README.md should be ignored), got %d", len(list))
	}
	if list[0].Name != "domour:k8s-pod-manager" {
		t.Fatalf("unexpected skill name: %s", list[0].Name)
	}

	snapshot, err := mgr.ResolveSkill(context.Background(), "domour:k8s-pod-manager")
	if err != nil {
		t.Fatalf("resolve skill: %v", err)
	}

	if snapshot.Name != "K8s Pod Manager" {
		t.Fatalf("expected name 'K8s Pod Manager', got '%s'", snapshot.Name)
	}
	if snapshot.Description != "查看、重启和排查 Pod 状态" {
		t.Fatalf("unexpected description: %s", snapshot.Description)
	}
	if snapshot.Instructions != "Focus on get and restart in cluster execution." {
		t.Fatalf("unexpected instructions: %s", snapshot.Instructions)
	}
	if len(snapshot.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(snapshot.Tools))
	}
	if snapshot.Tools[0].Name != "k8s.getPods" {
		t.Fatalf("expected tool 'k8s.getPods', got '%s'", snapshot.Tools[0].Name)
	}
	// Verify that parameters are parsed into valid json RawMessage
	if len(snapshot.Tools[0].Parameters) == 0 {
		t.Fatalf("expected parameters to be populated, got empty")
	}
}

func TestManagerReloadSkills(t *testing.T) {
	root := t.TempDir()
	// Create skill 1
	skill1Path := filepath.Join(root, "reload-test.md")
	os.WriteFile(skill1Path, []byte("# Reload Test Skill\n\n## Description\nSkill for reload test\n\n## Instructions\nTest reload\n"), 0o600)

	mgr := NewSkillManager()

	// Load initial skill
	if err := mgr.LoadSkillsFromDir(root); err != nil {
		t.Fatalf("initial load: %v", err)
	}

	list := mgr.ListSkills()
	if len(list) != 1 {
		t.Fatalf("expected 1 skill before reload, got %d", len(list))
	}

	// Resolve to get it loaded into cache
	_, err := mgr.ResolveSkill(context.Background(), "domour:reload-test")
	if err != nil {
		t.Fatalf("resolve before reload: %v", err)
	}

	// Verify it's loaded
	list = mgr.ListSkills()
	if !list[0].Loaded {
		t.Fatalf("expected skill to be loaded before reload")
	}

	// Reload clears skills and reloads from all default sources + configured dir
	mgr.ReloadSkills()

	list = mgr.ListSkills()
	// After ReloadSkills, we should have at least 0 skills (test env has no provider files)
	// Verify the reloaded skills don't include our temp file anymore since we cleared
	hasReloadTest := false
	for _, s := range list {
		if s.Name == "domour:reload-test" {
			hasReloadTest = true
			break
		}
	}
	if hasReloadTest {
		t.Fatalf("reloaded skills should not contain domour:reload-test since it was cleared")
	}

	// Now load from dir again to verify reload + load works
	if err := mgr.LoadSkillsFromDir(root); err != nil {
		t.Fatalf("load after reload: %v", err)
	}

	list = mgr.ListSkills()
	testSkillFound := false
	for _, s := range list {
		if s.Name == "domour:reload-test" {
			testSkillFound = true
			break
		}
	}
	if !testSkillFound {
		t.Fatalf("expected domour:reload-test in skills after reload+load, got %+v", list)
	}

	// Verify the skill is freshly resolved
	snapshot, err := mgr.ResolveSkill(context.Background(), "domour:reload-test")
	if err != nil {
		t.Fatalf("resolve after reload: %v", err)
	}
	if snapshot.Description != "Skill for reload test" {
		t.Fatalf("unexpected description after reload: %s", snapshot.Description)
	}
}

func TestSkillsPromptBuildingAndAutoDetection(t *testing.T) {
	root := t.TempDir()

	// Create skill 1: z-pod-manager
	skill1Dir := filepath.Join(root, "z-pod-manager")
	os.MkdirAll(skill1Dir, 0755)
	os.WriteFile(filepath.Join(skill1Dir, "SKILL.md"), []byte(`---
name: Z Pod Manager
description: Manage pods
intent_tags: ["k8s", "pod"]
---
Instructions for Z`), 0o600)

	// Create skill 2: a-database-helper
	skill2Dir := filepath.Join(root, "a-database-helper")
	os.MkdirAll(skill2Dir, 0755)
	os.WriteFile(filepath.Join(skill2Dir, "SKILL.md"), []byte(`---
name: A Database Helper
description: SQL optimization
intent_tags: ["sql", "database"]
---
Instructions for A`), 0o600)

	mgr := NewSkillManager()

	if err := mgr.LoadSkillsFromDir(root); err != nil {
		t.Fatalf("load: %v", err)
	}

	// 1. Verify sorting of available skills list
	avail, err := mgr.BuildAvailableSkillsPrompt(context.Background())
	if err != nil {
		t.Fatalf("available prompt: %v", err)
	}
	if !strings.Contains(avail, "a-database-helper") || !strings.Contains(avail, "z-pod-manager") {
		t.Fatalf("unexpected avail prompt: %s", avail)
	}
	// Check order: a-database-helper should come before z-pod-manager because it's sorted by Name alphabetically!
	aIdx := strings.Index(avail, "a-database-helper")
	zIdx := strings.Index(avail, "z-pod-manager")
	if aIdx > zIdx {
		t.Fatalf("expected alphabetical sorting for available skills prompt, got incorrect order")
	}

	// 2. Verify auto-detection of active skill by name
	matchedName := mgr.DetectActiveSkill(context.Background(), "I want to run a-database-helper commands")
	if matchedName != "domour:a-database-helper" {
		t.Fatalf("expected match 'domour:a-database-helper', got '%s'", matchedName)
	}

	// 3. Verify auto-detection of active skill by intent tags
	matchedTag := mgr.DetectActiveSkill(context.Background(), "How do I optimize my sql query?")
	if matchedTag != "domour:a-database-helper" {
		t.Fatalf("expected match 'domour:a-database-helper' by tag 'sql', got '%s'", matchedTag)
	}

	matchedTag2 := mgr.DetectActiveSkill(context.Background(), "restart that k8s pod please")
	if matchedTag2 != "domour:z-pod-manager" {
		t.Fatalf("expected match 'domour:z-pod-manager' by tag 'k8s/pod', got '%s'", matchedTag2)
	}

	// 4. Verify Active Skill Prompt building format
	activePrompt, err := mgr.BuildActiveSkillPrompt(context.Background(), "domour:z-pod-manager")
	if err != nil {
		t.Fatalf("active prompt: %v", err)
	}
	if !strings.Contains(activePrompt, "<active_skill>") || !strings.Contains(activePrompt, "Instructions for Z") || !strings.Contains(activePrompt, "id: domour:z-pod-manager") {
		t.Fatalf("unexpected active prompt content: %s", activePrompt)
	}
}

func TestSyncPublicSkills_HotUpdateAndUnregister(t *testing.T) {
	mgr := NewSkillManager()
	skillName := "DynamicHotSkill"
	defer publicskill.Unregister(skillName)

	publicskill.ReplaceByName(publicskill.Skill{
		ID:           "dyn-1",
		Name:         skillName,
		Description:  "v1 description",
		Instructions: "v1 instructions",
		Tools: []publicskill.ToolDefinition{
			{Name: "dyn.tool_v1", Description: "v1 tool"},
		},
	})
	mgr.SyncPublicSkills()

	snap1, err := mgr.ResolveSkill(context.Background(), skillName)
	if err != nil {
		t.Fatalf("ResolveSkill v1 failed: %v", err)
	}
	if snap1.Instructions != "v1 instructions" || len(snap1.Tools) != 1 || snap1.Tools[0].Name != "dyn.tool_v1" {
		t.Fatalf("unexpected v1 snapshot: %+v", snap1)
	}

	// Hot-update via ReplaceByName
	publicskill.ReplaceByName(publicskill.Skill{
		ID:           "dyn-1",
		Name:         skillName,
		Description:  "v2 description",
		Instructions: "v2 updated instructions",
		Tools: []publicskill.ToolDefinition{
			{Name: "dyn.tool_v2", Description: "v2 tool"},
		},
	})
	mgr.SyncPublicSkills()

	snap2, err := mgr.ResolveSkill(context.Background(), skillName)
	if err != nil {
		t.Fatalf("ResolveSkill v2 failed: %v", err)
	}
	if snap2.Instructions != "v2 updated instructions" || snap2.Description != "v2 description" || len(snap2.Tools) != 1 || snap2.Tools[0].Name != "dyn.tool_v2" {
		t.Fatalf("expected hot-updated v2 snapshot, got: %+v", snap2)
	}

	// Unregister and sync -> should be pruned
	publicskill.Unregister(skillName)
	mgr.SyncPublicSkills()

	if _, err := mgr.ResolveSkill(context.Background(), skillName); err == nil {
		t.Fatalf("expected unregistered public skill %q to be pruned from SkillManager", skillName)
	}
}

func TestResolveSkill_FileMtimeHotReload(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "hot-file-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	v1 := "---\nname: Hot File Skill\ndescription: v1\n---\nInstructions v1"
	if err := os.WriteFile(skillPath, []byte(v1), 0o600); err != nil {
		t.Fatalf("write v1: %v", err)
	}

	mgr := NewSkillManager()
	if err := mgr.LoadSkillsFromDir(root); err != nil {
		t.Fatalf("LoadSkillsFromDir: %v", err)
	}

	snap1, err := mgr.ResolveSkill(context.Background(), "domour:hot-file-skill")
	if err != nil {
		t.Fatalf("ResolveSkill v1: %v", err)
	}
	if snap1.Instructions != "Instructions v1" {
		t.Fatalf("expected 'Instructions v1', got %q", snap1.Instructions)
	}

	// Update file content and advance mtime
	v2 := "---\nname: Hot File Skill\ndescription: v2\n---\nInstructions v2 hot reloaded"
	if err := os.WriteFile(skillPath, []byte(v2), 0o600); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(skillPath, future, future)

	snap2, err := mgr.ResolveSkill(context.Background(), "domour:hot-file-skill")
	if err != nil {
		t.Fatalf("ResolveSkill v2: %v", err)
	}
	if snap2.Instructions != "Instructions v2 hot reloaded" {
		t.Fatalf("expected 'Instructions v2 hot reloaded', got %q", snap2.Instructions)
	}
}

func TestSessionScopedActiveSkillsIsolation(t *testing.T) {
	mgr := NewSkillManager()
	defer publicskill.Unregister("SkillAlpha")
	defer publicskill.Unregister("SkillBeta")

	publicskill.ReplaceByName(publicskill.Skill{
		Name:        "SkillAlpha",
		Description: "Alpha",
		IntentTags:  []string{"alpha_intent"},
		Tools:       []publicskill.ToolDefinition{{Name: "alpha.run"}},
	})
	publicskill.ReplaceByName(publicskill.Skill{
		Name:        "SkillBeta",
		Description: "Beta",
		IntentTags:  []string{"beta_intent"},
		Tools:       []publicskill.ToolDefinition{{Name: "beta.run"}},
	})

	ctxA := providerruntime.WithRequestMetadata(context.Background(), providerruntime.RequestMetadata{SessionID: "session-A"})
	ctxB := providerruntime.WithRequestMetadata(context.Background(), providerruntime.RequestMetadata{SessionID: "session-B"})

	if matched := mgr.DetectActiveSkill(ctxA, "trigger alpha_intent please"); matched != "SkillAlpha" {
		t.Fatalf("expected SkillAlpha for session A, got %q", matched)
	}
	// Session B detects a different skill (or none) and must NOT clear Session A's active skill!
	if matched := mgr.DetectActiveSkill(ctxB, "trigger beta_intent please"); matched != "SkillBeta" {
		t.Fatalf("expected SkillBeta for session B, got %q", matched)
	}

	_, unlockedA := mgr.FilterToolNames(ctxA)
	_, unlockedB := mgr.FilterToolNames(ctxB)

	if !unlockedA["alpha.run"] || unlockedA["beta.run"] {
		t.Fatalf("session A unlocked tools mismatch: %+v", unlockedA)
	}
	if !unlockedB["beta.run"] || unlockedB["alpha.run"] {
		t.Fatalf("session B unlocked tools mismatch: %+v", unlockedB)
	}
}
