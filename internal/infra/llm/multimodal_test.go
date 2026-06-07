package llm

import (
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestBuildUserInputMessageWithImageAttachment(t *testing.T) {
	t.Parallel()

	msg, err := BuildUserInputMessage("Describe the image.", []shared.BrainAttachment{
		{
			Filename: "diagram.png",
			MIMEType: "image/png",
			URL:      "https://example.com/diagram.png",
		},
	})
	if err != nil {
		t.Fatalf("buildUserInputMessage() error = %v", err)
	}
	if msg.Role != schema.User {
		t.Fatalf("message role = %q, want %q", msg.Role, schema.User)
	}
	if len(msg.UserInputMultiContent) != 2 {
		t.Fatalf("parts = %d, want 2", len(msg.UserInputMultiContent))
	}
	if msg.UserInputMultiContent[0].Type != schema.ChatMessagePartTypeText {
		t.Fatalf("first part type = %q, want text", msg.UserInputMultiContent[0].Type)
	}
	if msg.UserInputMultiContent[1].Type != schema.ChatMessagePartTypeImageURL {
		t.Fatalf("second part type = %q, want image_url", msg.UserInputMultiContent[1].Type)
	}
	if msg.UserInputMultiContent[1].Image == nil || msg.UserInputMultiContent[1].Image.URL == nil {
		t.Fatal("image part missing url")
	}
	if got := *msg.UserInputMultiContent[1].Image.URL; got != "https://example.com/diagram.png" {
		t.Fatalf("image url = %q, want %q", got, "https://example.com/diagram.png")
	}
}

func TestBuildUserInputMessageRejectsAudioAttachment(t *testing.T) {
	t.Parallel()

	_, err := BuildUserInputMessage("Transcribe this clip.", []shared.BrainAttachment{
		{
			Filename: "clip.wav",
			MIMEType: "audio/wav",
			URL:      "https://example.com/clip.wav",
		},
	})
	if err == nil {
		t.Fatal("buildUserInputMessage() error = nil, want rejection")
	}
	if !strings.Contains(err.Error(), "audio attachments are not supported yet") {
		t.Fatalf("error = %q, want audio rejection", err)
	}
}
