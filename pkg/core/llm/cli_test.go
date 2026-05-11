package llm

import (
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

func TestBuildCLIPromptRejectsImageInput(t *testing.T) {
	t.Parallel()

	model := &CLIChatModel{command: "copilot"}
	_, err := model.Generate(nil, []*schema.Message{
		{
			Role: schema.User,
			UserInputMultiContent: []schema.MessageInputPart{
				{
					Type: schema.ChatMessagePartTypeText,
					Text: "What is in this image?",
				},
				{
					Type:  schema.ChatMessagePartTypeImageURL,
					Image: &schema.MessageInputImage{},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("Generate() error = nil, want multimodal rejection")
	}
	if !strings.Contains(err.Error(), "does not support image inputs") {
		t.Fatalf("Generate() error = %q, want image rejection", err)
	}
}
