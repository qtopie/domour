package llamacpp

import (
	"bytes"
	"strings"
	"testing"
)

// TestCLI_LlamaCpp verifies [SPEC-LLAMA-005]
func TestCLI_LlamaCpp(t *testing.T) {
	// 1. Test "smoke" subcommand
	var bufSmoke bytes.Buffer
	err := RunCLIWithOutput([]string{"smoke"}, &bufSmoke)
	if err != nil {
		t.Fatalf("RunCLIWithOutput('smoke') failed: %v", err)
	}
	outputSmoke := bufSmoke.String()
	if !strings.Contains(outputSmoke, "All regression checks passed successfully") {
		t.Errorf("expected smoke test success message, got:\n%s", outputSmoke)
	}

	// 2. Test "generate" subcommand
	var bufGen bytes.Buffer
	err = RunCLIWithOutput([]string{"generate", "--prompt=Test prompt", "--tokens=5"}, &bufGen)
	if err != nil {
		t.Fatalf("RunCLIWithOutput('generate') failed: %v", err)
	}
	outputGen := bufGen.String()
	if !strings.Contains(outputGen, "completion_tokens") || !strings.Contains(outputGen, "content") {
		t.Errorf("expected JSON generate output, got:\n%s", outputGen)
	}

	// 3. Test "forward" subcommand
	var bufFwd bytes.Buffer
	err = RunCLIWithOutput([]string{"forward", "--prompt=Test forward logits", "--markers=0,2"}, &bufFwd)
	if err != nil {
		t.Fatalf("RunCLIWithOutput('forward') failed: %v", err)
	}
	outputFwd := bufFwd.String()
	if !strings.Contains(outputFwd, "logits") || !strings.Contains(outputFwd, "tokens_count") {
		t.Errorf("expected JSON forward output, got:\n%s", outputFwd)
	}

	// 4. Test "chat" subcommand
	var bufChat bytes.Buffer
	err = RunCLIWithOutput([]string{"chat", "--prompt=Hello bot", "--tokens=5"}, &bufChat)
	if err != nil {
		t.Fatalf("RunCLIWithOutput('chat') failed: %v", err)
	}
	outputChat := bufChat.String()
	if !strings.Contains(outputChat, "message") || !strings.Contains(outputChat, "assistant") {
		t.Errorf("expected JSON chat output, got:\n%s", outputChat)
	}

	// 5. Test "help" subcommand
	var bufHelp bytes.Buffer
	err = RunCLIWithOutput([]string{"help"}, &bufHelp)
	if err != nil {
		t.Fatalf("RunCLIWithOutput('help') failed: %v", err)
	}
	if !strings.Contains(bufHelp.String(), "Usage:") {
		t.Errorf("expected usage message, got:\n%s", bufHelp.String())
	}

	// 5. Test unknown subcommand
	var bufUnknown bytes.Buffer
	err = RunCLIWithOutput([]string{"unknown_cmd"}, &bufUnknown)
	if err == nil {
		t.Error("expected error for unknown subcommand, got nil")
	}
}
