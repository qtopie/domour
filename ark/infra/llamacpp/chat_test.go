package llamacpp

import (
	"testing"
)

func TestFormatChatML(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "User question"},
	}
	formatted := FormatChatML(msgs)
	expected := "<|im_start|>system\nSystem prompt<|im_end|>\n<|im_start|>user\nUser question<|im_end|>\n<|im_start|>assistant\n"
	if formatted != expected {
		t.Fatalf("expected:\n%s\ngot:\n%s", expected, formatted)
	}
}

func TestUTF8StreamBuffer_MultiByteRune(t *testing.T) {
	buf := NewUTF8StreamBuffer()

	// Chinese character "你" is 3 bytes in UTF-8: 0xE4 0xBD 0xA0
	fullBytes := []byte("你好")
	part1 := fullBytes[:2]  // Incomplete "你"
	part2 := fullBytes[2:4] // 3rd byte of "你" + 1st byte of "好"
	part3 := fullBytes[4:]  // remaining 2 bytes of "好"

	res1 := buf.Feed(part1)
	if res1 != "" {
		t.Errorf("expected empty string on incomplete rune, got %q", res1)
	}

	res2 := buf.Feed(part2)
	if res2 != "你" {
		t.Errorf("expected '你', got %q", res2)
	}

	res3 := buf.Feed(part3)
	if res3 != "好" {
		t.Errorf("expected '好', got %q", res3)
	}

	rem := buf.Flush()
	if rem != "" {
		t.Errorf("expected empty flush, got %q", rem)
	}
}

func TestStopDetector_SlidingWindow(t *testing.T) {
	detector := NewStopDetector([]string{"<|im_end|>", "\nUser:"})

	// Feed chunk without stop prefix
	emit1, stop1 := detector.Process("Hello world ")
	if stop1 || emit1 != "Hello world " {
		t.Errorf("chunk1: expected ('Hello world ', false), got (%q, %v)", emit1, stop1)
	}

	// Feed chunk that starts a potential stop prefix "<|im_"
	emit2, stop2 := detector.Process("<|im_")
	if stop2 || emit2 != "" {
		t.Errorf("chunk2: expected ('', false), got (%q, %v)", emit2, stop2)
	}

	// Feed remaining stop word "end|>"
	emit3, stop3 := detector.Process("end|>")
	if !stop3 || emit3 != "" {
		t.Errorf("chunk3: expected ('', true), got (%q, %v)", emit3, stop3)
	}
}

func TestStopDetector_FalseAlarmPrefix(t *testing.T) {
	detector := NewStopDetector([]string{"<|im_end|>"})

	emit1, stop1 := detector.Process("value <|im")
	if stop1 || emit1 != "value " {
		t.Errorf("chunk1: expected ('value ', false), got (%q, %v)", emit1, stop1)
	}

	// Next token is not "_end|>" but "age"
	emit2, stop2 := detector.Process("age")
	if stop2 || emit2 != "<|image" {
		t.Errorf("chunk2: expected ('<|image', false), got (%q, %v)", emit2, stop2)
	}

	rem := detector.FlushRemaining()
	if rem != "" {
		t.Errorf("expected empty remainder, got %q", rem)
	}
}
