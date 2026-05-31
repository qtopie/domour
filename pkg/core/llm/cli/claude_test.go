package cli

import (
	"context"
	"strings"
	"testing"

	providerruntime "github.com/qtopie/domour/internal/provider/runtime"
)

func TestClaudeProviderGetGenerateArgs(t *testing.T) {
	p := &claudeProvider{
		command:  "claude",
		model:    "sonnet",
		proxyURL: "http://127.0.0.1:1080",
	}

	runtime := &providerruntime.SessionRuntime{
		DomourSessionID:     "test-session-1234",
		Workspace:           "/tmp/my-workspace",
		ConversationStarted: true,
	}

	args, err := p.GetGenerateArgs(context.Background(), "Write a hello world program", []string{"assets/image1.png"}, runtime)
	if err != nil {
		t.Fatalf("GetGenerateArgs failed: %v", err)
	}

	// Helper to check if string exists in args
	hasArg := func(val string) bool {
		for _, arg := range args {
			if arg == val {
				return true
			}
		}
		return false
	}

	if !hasArg("--print") {
		t.Errorf("Expected --print in args")
	}

	if !hasArg("--dangerously-skip-permissions") {
		t.Errorf("Expected --dangerously-skip-permissions in args")
	}

	if !hasArg("--model") || !hasArg("sonnet") {
		t.Errorf("Expected model 'sonnet' in args")
	}

	if !hasArg("--add-dir") || !hasArg("/tmp/my-workspace") {
		t.Errorf("Expected add-dir '/tmp/my-workspace' in args")
	}

	// Verify session-id is passed and is a valid UUID
	sessionIDIndex := -1
	for i, arg := range args {
		if arg == "--session-id" {
			sessionIDIndex = i
			break
		}
	}
	if sessionIDIndex == -1 || sessionIDIndex+1 >= len(args) {
		t.Errorf("Expected --session-id option and value in args")
	} else {
		val := args[sessionIDIndex+1]
		if len(val) != 36 { // length of standard UUID string is 36
			t.Errorf("Expected valid UUID for session ID, got: %s", val)
		}
	}

	// Verify prompt is the last argument and contains the prompt and embedded asset paths
	lastArg := args[len(args)-1]
	if !strings.Contains(lastArg, "Write a hello world program") {
		t.Errorf("Expected prompt to contain input prompt")
	}
	if !strings.Contains(lastArg, "[Attached Assets/Images]") || !strings.Contains(lastArg, "assets/image1.png") {
		t.Errorf("Expected prompt to contain embedded asset paths")
	}
}
