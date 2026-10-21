package supervisor_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/qtopie/domour/ark/supervisor"
)

type mockProvider struct {
	name            string
	matchedSessions map[string]string
	turns           map[string][]*supervisor.TranscriptRecord
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) ParseStatusline(payload []byte) (*supervisor.StatuslineUpdate, error) {
	return nil, nil
}

func (m *mockProvider) ParseHookEvent(event string, payload []byte) (*supervisor.HookRequest, error) {
	return nil, nil
}

func (m *mockProvider) FormatHookDecision(req *supervisor.HookRequest, decision supervisor.ToolDecision, reason string) ([]byte, error) {
	return nil, nil
}

func (m *mockProvider) ParseTranscriptLine(line []byte) (*supervisor.TranscriptRecord, error) {
	return nil, nil
}

func (m *mockProvider) FormatInstruction(prompt string) ([]byte, error) {
	return []byte(prompt), nil
}

func (m *mockProvider) BuildHeadlessArgs(opts supervisor.LaunchOptions) ([]string, error) {
	return nil, nil
}

func (m *mockProvider) BuildResumeArgs(sessionID string, prompt string, opts supervisor.LaunchOptions) ([]string, error) {
	return nil, nil
}

func (m *mockProvider) MatchSession(sessionID string) (string, bool) {
	path, ok := m.matchedSessions[sessionID]
	return path, ok
}

func (m *mockProvider) ReadRecentTurns(sessionID string, maxTurns int) ([]*supervisor.TranscriptRecord, error) {
	records, ok := m.turns[sessionID]
	if !ok {
		return nil, fmt.Errorf("session not found")
	}
	if len(records) > maxTurns {
		return records[len(records)-maxTurns:], nil
	}
	return records, nil
}

func (m *mockProvider) DiscoverSessions() ([]*supervisor.AgentProcess, error) {
	return nil, nil
}

func (m *mockProvider) SpawnSession(ctx context.Context, req supervisor.SpawnRequest) (*supervisor.SpawnResult, error) {
	return nil, nil
}

func (m *mockProvider) InstructSession(ctx context.Context, req supervisor.InstructRequest) (*supervisor.InstructResult, error) {
	return nil, nil
}

// TestSupervisor_InspectMultiProviderRouting 验证 SPEC-SUPV-009 多 Provider 探测与路由机制
func TestSupervisor_InspectMultiProviderRouting(t *testing.T) {
	agyP := &mockProvider{
		name: "antigravity",
		matchedSessions: map[string]string{
			"sess-agy-1": "/tmp/brain/sess-agy-1/transcript.jsonl",
			"sess-shared": "/tmp/brain/sess-shared/transcript.jsonl",
		},
		turns: map[string][]*supervisor.TranscriptRecord{
			"sess-agy-1": {
				{StepIndex: 1, Role: "user", Content: "Hello AGY", Timestamp: time.Now()},
				{StepIndex: 2, Role: "assistant", Content: "Hi from AGY", Timestamp: time.Now()},
			},
			"sess-shared": {
				{StepIndex: 1, Role: "user", Content: "Conflict AGY", Timestamp: time.Now()},
			},
		},
	}

	copilotP := &mockProvider{
		name: "github-copilot",
		matchedSessions: map[string]string{
			"sess-copilot-1": "/tmp/copilot/sess-copilot-1/transcript.jsonl",
			"sess-shared":    "/tmp/copilot/sess-shared/transcript.jsonl",
		},
		turns: map[string][]*supervisor.TranscriptRecord{
			"sess-copilot-1": {
				{StepIndex: 1, Role: "user", Content: "Hello Copilot", Timestamp: time.Now()},
				{StepIndex: 2, Role: "assistant", Content: "Hi from Copilot", Timestamp: time.Now()},
			},
			"sess-shared": {
				{StepIndex: 1, Role: "user", Content: "Conflict Copilot", Timestamp: time.Now()},
			},
		},
	}

	providers := []supervisor.AgentProvider{agyP, copilotP}

	t.Run("Auto-discovery route to Antigravity", func(t *testing.T) {
		res, err := supervisor.InspectSession(providers, "", "sess-agy-1", 5)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.ProviderName != "antigravity" {
			t.Fatalf("expected antigravity, got %s", res.ProviderName)
		}
		if len(res.Records) != 2 {
			t.Fatalf("expected 2 records, got %d", len(res.Records))
		}
	})

	t.Run("Auto-discovery route to GitHub Copilot", func(t *testing.T) {
		res, err := supervisor.InspectSession(providers, "", "sess-copilot-1", 5)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.ProviderName != "github-copilot" {
			t.Fatalf("expected github-copilot, got %s", res.ProviderName)
		}
		if len(res.Records) != 2 {
			t.Fatalf("expected 2 records, got %d", len(res.Records))
		}
	})

	t.Run("Explicit provider flag routing", func(t *testing.T) {
		res, err := supervisor.InspectSession(providers, "antigravity", "sess-shared", 5)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.ProviderName != "antigravity" {
			t.Fatalf("expected antigravity, got %s", res.ProviderName)
		}

		res2, err := supervisor.InspectSession(providers, "github-copilot", "sess-shared", 5)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res2.ProviderName != "github-copilot" {
			t.Fatalf("expected github-copilot, got %s", res2.ProviderName)
		}
	})

	t.Run("ChatGPT and Codex alias routing", func(t *testing.T) {
		chatgptP := &mockProvider{
			name: "chatgpt",
			matchedSessions: map[string]string{
				"thr_shared_001": "/path/to/thr_shared_001",
			},
			turns: map[string][]*supervisor.TranscriptRecord{
				"thr_shared_001": {
					{StepIndex: 1, Role: "user", Content: "hello"},
				},
			},
		}
		allProviders := []supervisor.AgentProvider{agyP, chatgptP}

		// 显式指定 -p codex 能够成功命中 chatgpt
		resCodex, err := supervisor.InspectSession(allProviders, "codex", "thr_shared_001", 5)
		if err != nil {
			t.Fatalf("failed to route alias codex: %v", err)
		}
		if resCodex.ProviderName != "chatgpt" {
			t.Fatalf("expected chatgpt provider, got %s", resCodex.ProviderName)
		}

		// 显式指定 -p openai 能够成功命中 chatgpt
		resOpenAI, err := supervisor.InspectSession(allProviders, "openai", "thr_shared_001", 5)
		if err != nil {
			t.Fatalf("failed to route alias openai: %v", err)
		}
		if resOpenAI.ProviderName != "chatgpt" {
			t.Fatalf("expected chatgpt provider, got %s", resOpenAI.ProviderName)
		}
	})

	t.Run("Ambiguous conflict without explicit provider", func(t *testing.T) {
		_, err := supervisor.InspectSession(providers, "", "sess-shared", 5)
		if err == nil {
			t.Fatalf("expected ambiguous conflict error, got nil")
		}
	})

	t.Run("Session not found in any provider", func(t *testing.T) {
		_, err := supervisor.InspectSession(providers, "", "non-existent-session", 5)
		if err == nil {
			t.Fatalf("expected not found error, got nil")
		}
	})

	t.Run("Supervisor facade integration", func(t *testing.T) {
		sup, err := supervisor.New(supervisor.Config{})
		if err != nil {
			t.Fatalf("failed to create supervisor: %v", err)
		}
		defer sup.Stop()

		_ = sup.RegisterProvider(agyP)
		_ = sup.RegisterProvider(copilotP)

		res, err := sup.InspectSession(context.Background(), "", "sess-agy-1", 5)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.ProviderName != "antigravity" {
			t.Fatalf("expected antigravity, got %s", res.ProviderName)
		}
	})
}
