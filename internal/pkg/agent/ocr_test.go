package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/pkg/brain/diencephalon"
)

func TestWantsOCRTask(t *testing.T) {
	t.Parallel()

	cases := []string{
		"请对这张图片做OCR并提取文字",
		"extract text from this image",
		"帮我识别图中文字",
	}
	for _, input := range cases {
		if !wantsOCRTask(input) {
			t.Fatalf("wantsOCRTask(%q) = false, want true", input)
		}
	}
	if wantsOCRTask("describe this architecture screenshot") {
		t.Fatal("wantsOCRTask() = true, want false for non-OCR prompt")
	}
}

func TestBuildChatPromptIncludesOCRInstructions(t *testing.T) {
	t.Parallel()

	prompt := buildChatPrompt("请识别图片中的文字", "", "", "", "")
	if !strings.Contains(prompt, "Task mode: OCR") {
		t.Fatalf("buildChatPrompt() = %q, want OCR task mode", prompt)
	}
	if !strings.Contains(prompt, "Extract visible text faithfully.") {
		t.Fatalf("buildChatPrompt() = %q, want OCR requirements", prompt)
	}
}

func TestApplyChatInterceptionContextIncludesOCRFacts(t *testing.T) {
	t.Parallel()

	prompt := applyChatInterceptionContext("User request:\n请分析图片", &ChatInterception{
		Source:   "ollama",
		Summary:  "A receipt screenshot.",
		KeyFacts: []string{"total_amount=138", "order_id=A9021"},
		OCRText:  "Total 138\nOrder A9021",
	})
	if !strings.Contains(prompt, "Motor context interception:") {
		t.Fatalf("applyChatInterceptionContext() = %q, want interception header", prompt)
	}
	if !strings.Contains(prompt, "total_amount=138") {
		t.Fatalf("applyChatInterceptionContext() = %q, want key facts", prompt)
	}
	if !strings.Contains(prompt, "OCR evidence:\nTotal 138") {
		t.Fatalf("applyChatInterceptionContext() = %q, want OCR evidence", prompt)
	}
}

func TestBuildChatSystemPromptIncludesInterceptionNote(t *testing.T) {
	t.Parallel()

	prompt := buildChatSystemPrompt("请识别图中文字", []BrainAttachment{{Filename: "receipt.png", MIMEType: "image/png"}}, &ChatInterception{
		Source:  "ollama",
		Summary: "A receipt screenshot.",
	})
	if !strings.Contains(prompt, "motor-side context interception pass") {
		t.Fatalf("buildChatSystemPrompt() = %q, want interception note", prompt)
	}
}

func TestParseChatInterception(t *testing.T) {
	t.Parallel()

	interception := parseChatInterception(`
SUMMARY:
Receipt screenshot
KEY_FACTS:
- total_amount=138
- order_id=A9021
OCR_TEXT:
Total 138
Order A9021
`)
	if interception == nil {
		t.Fatal("parseChatInterception() = nil, want result")
	}
	if interception.Summary != "Receipt screenshot" {
		t.Fatalf("Summary = %q, want %q", interception.Summary, "Receipt screenshot")
	}
	if len(interception.KeyFacts) != 2 {
		t.Fatalf("len(KeyFacts) = %d, want 2", len(interception.KeyFacts))
	}
	if !strings.Contains(interception.OCRText, "Order A9021") {
		t.Fatalf("OCRText = %q, want OCR lines", interception.OCRText)
	}
}

func TestChatContextWorkingSetSemanticVersion(t *testing.T) {
	t.Parallel()

	store := newChatContextWorkingSet(16, time.Minute)
	first := store.Update("session-1", 7, &ChatInterception{
		Summary:  "Receipt screenshot",
		KeyFacts: []string{"total_amount=138"},
		OCRText:  "Total 138",
	})
	if first.RawVersion != 1 || first.SemanticVersion != 1 {
		t.Fatalf("first update = %#v, want raw=1 semantic=1", first)
	}

	second := store.Update("session-1", 7, &ChatInterception{
		OCRText: "Total 138\nThank you for shopping",
	})
	if second.RawVersion != 2 {
		t.Fatalf("second raw version = %d, want 2", second.RawVersion)
	}
	if second.SemanticVersion != 1 {
		t.Fatalf("second semantic version = %d, want 1", second.SemanticVersion)
	}

	third := store.Update("session-1", 7, &ChatInterception{
		KeyFacts: []string{"total_amount=188"},
	})
	if third.SemanticVersion != 2 {
		t.Fatalf("third semantic version = %d, want 2", third.SemanticVersion)
	}
}

func TestLocalBrainClientChatReplyUsesLatestSemanticContext(t *testing.T) {
	t.Cleanup(resetChatContextWorkingSetForTest)
	resetChatContextWorkingSetForTest()

	callCount := 0
	brain := &localBrainClient{
		chatModel: &fakeChatModel{
			generateText: func(_ context.Context, messages []*schema.Message) (diencephalon.Response, error) {
				callCount++
				if callCount == 1 {
					defaultChatContextWorkingSet.Update("session-1", 11, &ChatInterception{
						Summary:  "Receipt screenshot",
						KeyFacts: []string{"total_amount=138"},
						OCRText:  "Total 138",
					})
					return diencephalon.Response{Content: "stale", Provider: "ollama", Model: "gemma4"}, nil
				}
				last := messages[len(messages)-1]
				if len(last.UserInputMultiContent) == 0 || !strings.Contains(last.UserInputMultiContent[0].Text, "total_amount=138") {
					t.Fatalf("second iteration prompt = %#v, want latest key fact", last.UserInputMultiContent)
				}
				return diencephalon.Response{Content: "fresh", Provider: "ollama", Model: "gemma4"}, nil
			},
		},
	}

	reply, err := brain.ChatReply(context.Background(), BrainChatRequest{
		SessionID:   "session-1",
		Seq:         11,
		Message:     "请分析图片里的金额",
		Attachments: []BrainAttachment{{Filename: "receipt.png", MIMEType: "image/png", DataBase64: "aGVsbG8="}},
	})
	if err != nil {
		t.Fatalf("ChatReply() error = %v", err)
	}
	if callCount != 2 {
		t.Fatalf("GenerateText() call count = %d, want 2", callCount)
	}
	if reply.Content != "fresh" {
		t.Fatalf("reply content = %q, want %q", reply.Content, "fresh")
	}
}

type fakeChatModel struct {
	generateText func(context.Context, []*schema.Message) (diencephalon.Response, error)
}

func (m *fakeChatModel) Provider() string { return "test" }
func (m *fakeChatModel) Model() string    { return "test" }
func (m *fakeChatModel) GenerateMessage(context.Context, []*schema.Message) (*schema.Message, error) {
	return nil, nil
}
func (m *fakeChatModel) GenerateText(ctx context.Context, messages []*schema.Message) (diencephalon.Response, error) {
	return m.generateText(ctx, messages)
}
func (m *fakeChatModel) BindTools([]*schema.ToolInfo) error { return nil }
