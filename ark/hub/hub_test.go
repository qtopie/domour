package hub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/qtopie/domour/internal/bionic/tool"
	"github.com/qtopie/domour/internal/cognitor/proxy"
	"github.com/qtopie/domour/internal/config"
)

func TestArkHubToolsAndSkills(t *testing.T) {
	tm := tool.NewManager(tool.WithCleanupInterval(0))
	defer tm.Close()

	// Register a dummy tool with internal kind
	err := tm.Register(tool.ToolSpec{
		Name:        "test.tool",
		Kind:        tool.ToolKindInternal,
		Description: "A dummy test tool",
		Load: func(ctx context.Context) (tool.ToolRuntime, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("register test tool: %v", err)
	}

	resolver := &bionicToolResolver{tm: tm}
	h := NewArkHubWithResolver(resolver)
	ctx := context.Background()

	t.Run("List and Get Tools", func(t *testing.T) {
		list, err := h.ListTools(ctx)
		if err != nil {
			t.Fatalf("ListTools failed: %v", err)
		}
		if len(list) != 1 || list[0].Name != "test.tool" {
			t.Errorf("unexpected tools list: %+v", list)
		}

		tool, err := h.GetTool(ctx, "test.tool")
		if err != nil {
			t.Fatalf("GetTool failed: %v", err)
		}
		if tool.Description != "A dummy test tool" {
			t.Errorf("unexpected tool description: %q", tool.Description)
		}

		_, err = h.GetTool(ctx, "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent tool, got nil")
		}
	})

	t.Run("List and Get Skills", func(t *testing.T) {
		// No skills registered yet
		list, err := h.ListSkills(ctx)
		if err != nil {
			t.Fatalf("ListSkills failed: %v", err)
		}
		if len(list) != 0 {
			t.Errorf("expected empty skills list, got: %+v", list)
		}

		_, err = h.GetSkill(ctx, "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent skill, got nil")
		}
	})
}

func TestArkHubProviderManager(t *testing.T) {
	// Set HOME environment variable to a temp directory for config redirection
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Ensure config directory exists
	err := os.MkdirAll(filepath.Join(tmpDir, ".domour"), 0755)
	if err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// Trigger reloading config from the redirected HOME
	_, _ = config.ReloadDomourConfig()

	tm := tool.NewManager(tool.WithCleanupInterval(0))
	defer tm.Close()

	h := NewArkHub()
	ctx := context.Background()

	t.Run("Save, Get, and List Providers", func(t *testing.T) {
		p := &ProviderInfo{
			ID:      "openai",
			BaseURL: "https://api.openai.com/v1",
			APIKey:  "sk-test",
			Enabled: true,
		}

		err := h.SaveProvider(ctx, p)
		if err != nil {
			t.Fatalf("SaveProvider failed: %v", err)
		}

		// Reload config state in the package to sync it
		_, _ = config.ReloadDomourConfig()

		got, err := h.GetProvider(ctx, "openai")
		if err != nil {
			t.Fatalf("GetProvider failed: %v", err)
		}
		if got.APIKey != "sk-test" || !got.Enabled {
			t.Errorf("unexpected provider details: %+v", got)
		}
		// Default health state should be healthy
		if !got.Healthy {
			t.Errorf("expected provider to default to healthy")
		}

		// Mock unhealthy provider status
		proxy.SetProviderHealth("openai", false, fmt.Errorf("mocked connection timeout"))

		gotUnhealthy, err := h.GetProvider(ctx, "openai")
		if err != nil {
			t.Fatalf("GetProvider failed: %v", err)
		}
		if gotUnhealthy.Healthy {
			t.Errorf("expected provider to be unhealthy")
		}
		if gotUnhealthy.LastError != "mocked connection timeout" {
			t.Errorf("expected last error 'mocked connection timeout', got %q", gotUnhealthy.LastError)
		}

		list, err := h.ListProviders(ctx)
		if err != nil {
			t.Fatalf("ListProviders failed: %v", err)
		}
		
		foundOpenAI := false
		for _, item := range list {
			if item.ID == "openai" {
				foundOpenAI = true
				if item.APIKey != "sk-test" {
					t.Errorf("expected apiKey 'sk-test', got %q", item.APIKey)
				}
				if item.Healthy {
					t.Errorf("expected item to be unhealthy in list")
				}
				if item.LastError != "mocked connection timeout" {
					t.Errorf("expected item error in list, got %q", item.LastError)
				}
			}
		}
		if !foundOpenAI {
			t.Errorf("expected 'openai' to be in the provider list: %+v", list)
		}
	})

	t.Run("Toggle Provider Status", func(t *testing.T) {
		err := h.ToggleProviderStatus(ctx, "openai", false)
		if err != nil {
			t.Fatalf("ToggleProviderStatus failed: %v", err)
		}

		_, _ = config.ReloadDomourConfig()

		got, err := h.GetProvider(ctx, "openai")
		if err != nil {
			t.Fatalf("GetProvider failed: %v", err)
		}
		if got.Enabled {
			t.Error("expected provider to be disabled")
		}
	})
}

type bionicToolResolver struct {
	tm *tool.Manager
}

func (r *bionicToolResolver) ListTools(ctx context.Context) ([]*ToolManifest, error) {
	tools := r.tm.List()
	manifests := make([]*ToolManifest, len(tools))
	for i, t := range tools {
		manifests[i] = &ToolManifest{
			Name:        t.Name,
			Kind:        string(t.Kind),
			Description: t.Description,
			Loaded:      t.Loaded,
			Meta:        t.Meta,
		}
	}
	return manifests, nil
}

func (r *bionicToolResolver) GetTool(ctx context.Context, id string) (*ToolManifest, error) {
	tools := r.tm.List()
	for _, t := range tools {
		if t.Name == id {
			return &ToolManifest{
				Name:        t.Name,
				Kind:        string(t.Kind),
				Description: t.Description,
				Loaded:      t.Loaded,
				Meta:        t.Meta,
			}, nil
		}
	}
	return nil, fmt.Errorf("tool %q not found", id)
}

func (r *bionicToolResolver) ListSkills(ctx context.Context) ([]*SkillManifest, error) {
	skills := r.tm.ListSkills()
	manifests := make([]*SkillManifest, len(skills))
	for i, s := range skills {
		manifests[i] = &SkillManifest{
			Name:        s.Name,
			Description: s.Description,
		}
	}
	return manifests, nil
}

func (r *bionicToolResolver) GetSkill(ctx context.Context, id string) (*SkillManifest, error) {
	s, err := r.tm.ResolveSkill(ctx, id)
	if err != nil {
		return nil, err
	}
	return &SkillManifest{
		Name:        s.Name,
		Description: s.Description,
	}, nil
}

