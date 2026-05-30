package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestApplyProxyEnv(t *testing.T) {
	t.Parallel()

	env := applyProxyEnv([]string{"PATH=/bin"}, "http://192.168.50.31:1080")
	joined := strings.Join(env, "\n")

	for _, expected := range []string{
		"HTTPS_PROXY=http://192.168.50.31:1080",
		"https_proxy=http://192.168.50.31:1080",
		"HTTP_PROXY=http://192.168.50.31:1080",
		"http_proxy=http://192.168.50.31:1080",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("applyProxyEnv() missing %q in %q", expected, joined)
		}
	}
}

func TestBuildCLIPromptSupportsMultimodal(t *testing.T) {
	t.Parallel()

	model := &CLIChatModel{command: "copilot"}
	prompt, attachments, err := model.buildCLIPrompt([]*schema.Message{
		{
			Role: schema.User,
			UserInputMultiContent: []schema.MessageInputPart{
				{
					Type: schema.ChatMessagePartTypeText,
					Text: "What is in this image?",
				},
				{
					Type: schema.ChatMessagePartTypeImageURL,
					Image: &schema.MessageInputImage{
						MessagePartCommon: schema.MessagePartCommon{
							MIMEType: "image/png",
							URL:      stringPtr("file:///tmp/test.png"),
						},
					},
				},
				{
					Type: schema.ChatMessagePartTypeImageURL,
					Image: &schema.MessageInputImage{
						MessagePartCommon: schema.MessagePartCommon{
							MIMEType:   "image/png",
							Base64Data: stringPtr("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="),
						},
					},
				},
			},
		},
	})

	if err != nil {
		t.Fatalf("buildCLIPrompt() error = %v", err)
	}
	if !strings.Contains(prompt, "What is in this image?") {
		t.Fatalf("prompt = %q, want it to contain text", prompt)
	}
	if len(attachments) != 2 {
		t.Fatalf("len(attachments) = %d, want 2", len(attachments))
	}
	if attachments[0].URL == nil || *attachments[0].URL != "file:///tmp/test.png" {
		t.Fatalf("attachment 0 URL = %v, want file:///tmp/test.png", attachments[0].URL)
	}
}

func stringPtr(s string) *string {
	return &s
}

func TestCLIChatModelIsReady(t *testing.T) {
	// Use 'true' as a mock command that should exist on most systems
	m, err := New(&Config{
		Provider: "qodercli",
		Command:  "true",
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	cliModel := m.(*CLIChatModel)
	cliModel.performHealthCheck()

	ready, err := cliModel.IsReady(context.Background())
	if !ready {
		t.Fatalf("IsReady() = false, want true for 'true' command")
	}
	if err != nil {
		t.Fatalf("IsReady() error = %v, want nil", err)
	}

	// Test with non-existent command
	_, err = New(&Config{
		Provider: "qodercli",
		Command:  "non-existent-command-xyz",
	})
	if err == nil {
		t.Fatalf("New() should fail for non-existent command")
	}
}

func TestVProxyWrappingIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	traceFile := filepath.Join(tmpDir, "trace.log")

	// Create mock vproxy binary
	vproxyName := "vproxy"
	var scriptContent string
	if filepath.Separator == '\\' {
		// Windows batch script
		vproxyName = "vproxy.bat"
		scriptContent = fmt.Sprintf(`@echo off
echo ARGS: %%* >> "%s"
:loop
if "%%~1"=="" goto end
if "%%~1"=="-c" (
    echo CONFIG_CONTENT: >> "%s"
    type "%%~2" >> "%s"
    shift
    shift
    goto loop
)
shift
goto loop
:end
`, traceFile, traceFile, traceFile)
	} else {
		// Unix shell script
		scriptContent = fmt.Sprintf(`#!/bin/bash
echo "ARGS: $*" >> "%s"
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -c)
      echo "CONFIG_CONTENT:" >> "%s"
      cat "$2" >> "%s"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
`, traceFile, traceFile, traceFile)
	}

	vproxyPath := filepath.Join(tmpDir, vproxyName)
	err := os.WriteFile(vproxyPath, []byte(scriptContent), 0755)
	if err != nil {
		t.Fatalf("failed to write mock vproxy script: %v", err)
	}

	// Backup and modify PATH
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)

	newPath := tmpDir + string(filepath.ListSeparator) + originalPath
	os.Setenv("PATH", newPath)

	// Create CLI model with proxy configured and command set to "true" (or "cmd.exe" on Windows)
	cmd := "true"
	if filepath.Separator == '\\' {
		cmd = "cmd.exe"
	}

	m, err := New(&Config{
		Provider: "qodercli",
		Command:  cmd,
		ProxyURL: "socks5://127.0.0.1:9999",
	})
	if err != nil {
		t.Fatalf("failed to create CLI Chat Model: %v", err)
	}

	cliModel := m.(*CLIChatModel)
	cliModel.performHealthCheck()

	// Verify trace file exists and contains correct content
	traceData, err := os.ReadFile(traceFile)
	if err != nil {
		t.Fatalf("failed to read trace file: %v. Integration test failed to invoke wrapped mock vproxy.", err)
	}

	traceStr := string(traceData)
	t.Logf("Mock Vproxy output trace:\n%s", traceStr)

	if !strings.Contains(traceStr, "ARGS:") {
		t.Fatalf("expected trace to contain ARGS, but got:\n%s", traceStr)
	}
	if !strings.Contains(traceStr, "socks5://127.0.0.1:9999") {
		t.Fatalf("expected trace to contain upstream proxy socks5://127.0.0.1:9999, but got:\n%s", traceStr)
	}
	if !strings.Contains(traceStr, "CONFIG_CONTENT:") {
		t.Fatalf("expected trace to contain CONFIG_CONTENT:, but got:\n%s", traceStr)
	}
}
