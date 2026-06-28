package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMemoryRegistry(t *testing.T) {
	ctx := context.Background()
	reg := NewMemoryRegistry()

	s := &Skill{
		ID:          "test-skill",
		Name:        "Test Skill",
		Description: "A transient test skill",
	}

	if err := reg.Register(ctx, s); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	retrieved, err := reg.Get(ctx, "test-skill")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.Name != "Test Skill" {
		t.Errorf("Expected Test Skill, got %s", retrieved.Name)
	}

	list, err := reg.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("Expected 1 skill in list, got %d", len(list))
	}

	if err := reg.Delete(ctx, "test-skill"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = reg.Get(ctx, "test-skill")
	if err == nil {
		t.Error("Expected error after Delete, got nil")
	}
}

func TestFileRegistry(t *testing.T) {
	ctx := context.Background()
	tmpDir, err := os.MkdirTemp("", "domour-skills-test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	reg := NewFileRegistry(tmpDir)

	s := &Skill{
		ID:          "file-skill",
		Name:        "File Skill",
		Description: "A filesystem stored skill",
	}

	if err := reg.Register(ctx, s); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Verify JSON file exists
	jsonPath := filepath.Join(tmpDir, "file-skill.json")
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("Expected JSON file %s, got err: %v", jsonPath, err)
	}

	retrieved, err := reg.Get(ctx, "file-skill")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if retrieved.Name != "File Skill" {
		t.Errorf("Expected File Skill, got %s", retrieved.Name)
	}

	list, err := reg.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("Expected 1 skill in list, got %d", len(list))
	}

	if err := reg.Delete(ctx, "file-skill"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = reg.Get(ctx, "file-skill")
	if err == nil {
		t.Error("Expected error after Delete, got nil")
	}
}

func TestCompositeRegistry(t *testing.T) {
	ctx := context.Background()
	reg1 := NewMemoryRegistry()
	reg2 := NewMemoryRegistry()
	composite := NewCompositeRegistry(reg1, reg2)

	s := &Skill{
		ID:          "composite-skill",
		Name:        "Composite Skill",
		Description: "Distributed replication skill",
	}

	if err := composite.Register(ctx, s); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Should be registered in both backends
	s1, err1 := reg1.Get(ctx, "composite-skill")
	s2, err2 := reg2.Get(ctx, "composite-skill")
	if err1 != nil || err2 != nil {
		t.Fatalf("Get from backing registries failed: %v, %v", err1, err2)
	}
	if s1.Name != "Composite Skill" || s2.Name != "Composite Skill" {
		t.Errorf("Unexpected skill names: s1=%s, s2=%s", s1.Name, s2.Name)
	}

	// Delete from composite should delete from all
	if err := composite.Delete(ctx, "composite-skill"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err1 = reg1.Get(ctx, "composite-skill")
	_, err2 = reg2.Get(ctx, "composite-skill")
	if err1 == nil || err2 == nil {
		t.Error("Expected composite delete to remove skill from all backing registries")
	}
}
