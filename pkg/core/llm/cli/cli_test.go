package cli

import (
	"context"
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
