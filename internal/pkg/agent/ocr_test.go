package agent

import (
	"context"
	"strings"
	"testing"
	"time"
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

func TestWaitForInitialChatInterception(t *testing.T) {
	previous := initialChatInterceptionWait
	initialChatInterceptionWait = 20 * time.Millisecond
	defer func() {
		initialChatInterceptionWait = previous
	}()

	bridge := newSessionBridge()
	bridge.Interception <- ChatInterception{Summary: "receipt"}
	req := waitForInitialChatInterception(context.Background(), BrainChatRequest{
		Message:     "analyze",
		Attachments: []BrainAttachment{{Filename: "receipt.png", MIMEType: "image/png"}},
	}, bridge)
	if req.Interception == nil || req.Interception.Summary != "receipt" {
		t.Fatalf("waitForInitialChatInterception() = %#v, want interception", req.Interception)
	}
}
