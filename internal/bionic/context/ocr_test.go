package context

import (
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"strings"
	"testing"
	"time"
)

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
CONFIDENCE_SCORE:
0.85
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
	if interception.Confidence != 0.85 {
		t.Fatalf("Confidence = %v, want 0.85", interception.Confidence)
	}
}

func TestOCRConfidenceThreshold(t *testing.T) {
	t.Parallel()

	interceptionLow := &shared.ChatInterception{
		OCRText:    "Blurry text",
		Confidence: 0.3,
	}
	promptLow := ApplyChatInterceptionContext("Analyze this", interceptionLow)
	if strings.Contains(promptLow, "Blurry text") {
		t.Fatal("ApplyChatInterceptionContext should drop low confidence evidence")
	}

	interceptionHigh := &shared.ChatInterception{
		OCRText:    "Clear text",
		Confidence: 0.9,
	}
	promptHigh := ApplyChatInterceptionContext("Analyze this", interceptionHigh)
	if !strings.Contains(promptHigh, "Clear text") {
		t.Fatal("ApplyChatInterceptionContext should include high confidence evidence")
	}
}

func TestChatContextWorkingSetSemanticVersion(t *testing.T) {
	t.Parallel()

	store := newChatContextWorkingSet(16, time.Minute)
	first := store.Update("session-1", 7, &shared.ChatInterception{
		Summary:  "Receipt screenshot",
		KeyFacts: []string{"total_amount=138"},
		OCRText:  "Total 138",
	})
	if first.RawVersion != 1 || first.SemanticVersion != 1 {
		t.Fatalf("first update = %#v, want raw=1 semantic=1", first)
	}

	second := store.Update("session-1", 7, &shared.ChatInterception{
		OCRText: "Total 138\nThank you for shopping",
	})
	if second.RawVersion != 2 {
		t.Fatalf("second raw version = %d, want 2", second.RawVersion)
	}
	if second.SemanticVersion != 1 {
		t.Fatalf("second semantic version = %d, want 1", second.SemanticVersion)
	}

	third := store.Update("session-1", 7, &shared.ChatInterception{
		KeyFacts: []string{"total_amount=188"},
	})
	if third.SemanticVersion != 2 {
		t.Fatalf("third semantic version = %d, want 2", third.SemanticVersion)
	}
}
