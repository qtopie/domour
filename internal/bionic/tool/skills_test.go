package tool

import (
	"context"
	"os"
	"path/filepath"
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
