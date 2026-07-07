package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	manager := NewManager(WithCleanupInterval(0))
	defer manager.Close()

	if err := manager.LoadSkillsFromDir(root); err != nil {
		t.Fatalf("load skills: %v", err)
	}

	list := manager.ListSkills()
	if len(list) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(list))
	}
	if list[0].Name != "domour:todo" {
		t.Fatalf("unexpected skill name: %s", list[0].Name)
	}

	snapshot, err := manager.ResolveSkill(context.Background(), "domour:todo")
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
	manager := NewManager(WithCleanupInterval(0))
	defer manager.Close()

	spec := NewFileSkill(writeTempSkill(t))
	spec.Name = "domour:todo"
	spec.IdleTTL = time.Millisecond
	if err := manager.RegisterSkill(spec); err != nil {
		t.Fatalf("register skill: %v", err)
	}

	text, err := manager.BuildSkillInstruction(context.Background(), "domour:todo")
	if err != nil {
		t.Fatalf("build skill instruction: %v", err)
	}
	if text == "" {
		t.Fatalf("expected non-empty skill instruction")
	}

	time.Sleep(5 * time.Millisecond)
	manager.UnloadIdleSkills()

	list := manager.ListSkills()
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

	manager := NewManager(WithCleanupInterval(0))
	defer manager.Close()
	for _, spec := range append(append(gemini, copilot...), claude...) {
		if err := manager.RegisterSkill(spec); err != nil {
			t.Fatalf("register provider skill %s: %v", spec.Name, err)
		}
	}

	snapshot, err := manager.ResolveSkill(context.Background(), "gemini:gemini")
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

	manager := NewManager(WithCleanupInterval(0))
	defer manager.Close()

	if err := manager.LoadSkillsFromDir(root); err != nil {
		t.Fatalf("load skills: %v", err)
	}

	list := manager.ListSkills()
	if len(list) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(list))
	}
	if list[0].Name != "domour:k8s-pod" {
		t.Fatalf("unexpected skill name: %s", list[0].Name)
	}

	snapshot, err := manager.ResolveSkill(context.Background(), "domour:k8s-pod")
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

	manager := NewManager(WithCleanupInterval(0))
	defer manager.Close()

	if err := manager.LoadSkillsFromDir(root); err != nil {
		t.Fatalf("load skills: %v", err)
	}

	list := manager.ListSkills()
	if len(list) != 1 {
		t.Fatalf("expected 1 skill (README.md should be ignored), got %d", len(list))
	}
	if list[0].Name != "domour:k8s-pod-manager" {
		t.Fatalf("unexpected skill name: %s", list[0].Name)
	}

	snapshot, err := manager.ResolveSkill(context.Background(), "domour:k8s-pod-manager")
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

	manager := NewManager(WithCleanupInterval(0))
	defer manager.Close()

	if err := manager.LoadSkillsFromDir(root); err != nil {
		t.Fatalf("load: %v", err)
	}

	// 1. Verify sorting of available skills list
	avail, err := manager.BuildAvailableSkillsPrompt(context.Background())
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
	matchedName := manager.DetectActiveSkill(context.Background(), "I want to run a-database-helper commands")
	if matchedName != "domour:a-database-helper" {
		t.Fatalf("expected match 'domour:a-database-helper', got '%s'", matchedName)
	}

	// 3. Verify auto-detection of active skill by intent tags
	matchedTag := manager.DetectActiveSkill(context.Background(), "How do I optimize my sql query?")
	if matchedTag != "domour:a-database-helper" {
		t.Fatalf("expected match 'domour:a-database-helper' by tag 'sql', got '%s'", matchedTag)
	}

	matchedTag2 := manager.DetectActiveSkill(context.Background(), "restart that k8s pod please")
	if matchedTag2 != "domour:z-pod-manager" {
		t.Fatalf("expected match 'domour:z-pod-manager' by tag 'k8s/pod', got '%s'", matchedTag2)
	}

	// 4. Verify Active Skill Prompt building format
	activePrompt, err := manager.BuildActiveSkillPrompt(context.Background(), "domour:z-pod-manager")
	if err != nil {
		t.Fatalf("active prompt: %v", err)
	}
	if !strings.Contains(activePrompt, "<active_skill>") || !strings.Contains(activePrompt, "Instructions for Z") || !strings.Contains(activePrompt, "id: domour:z-pod-manager") {
		t.Fatalf("unexpected active prompt content: %s", activePrompt)
	}
}

