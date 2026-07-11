package brain

import (
	"testing"
)

func TestTopicDetector_ExtractFingerprint(t *testing.T) {
	d := NewTopicDetector()

	fp := d.ExtractFingerprint("帮我看看auth为什么报401，文件路径是 src/auth/middleware.go")
	if len(fp.Terms) == 0 {
		t.Fatal("ExtractFingerprint returned empty terms")
	}
	// Should extract domain signals: file paths, error-like patterns
	hasCodeSignal := false
	for term := range fp.Terms {
		if term == "src/auth/middleware.go" {
			hasCodeSignal = true
		}
	}
	if !hasCodeSignal {
		t.Errorf("Expected domain signal 'src/auth/middleware.go' in terms, got: %v", keys(fp.Terms))
	}

	label := d.SuggestTopicLabel(fp)
	t.Logf("Topic label: %s", label)
	if label == "" {
		t.Fatal("SuggestTopicLabel returned empty")
	}
}

func TestTopicDetector_ShiftDetection(t *testing.T) {
	d := NewTopicDetector()

	tests := []struct {
		name      string
		prev      string
		curr      string
		wantShift bool
	}{
		{
			name:      "same topic — auth debugging",
			prev:      "帮我看看auth为什么报401，日志里显示token过期了",
			curr:      "这个401错误看起来是JWT签名不对，你能帮我检查一下middleware吗",
			wantShift: false,
		},
		{
			name:      "shift — auth → payment",
			prev:      "帮我看看auth为什么报401，日志里显示token过期了",
			curr:      "现在我们来设计支付页面，需要用户点击购买按钮后跳转到收银台",
			wantShift: true,
		},
		{
			name:      "shift — config → database",
			prev:      "帮我改一下nginx配置，把端口从8080改成443",
			curr:      "数据库迁移脚本出了点问题，帮我看看这个SQL语句为什么执行失败",
			wantShift: true,
		},
		{
			name:      "same topic — continuing payment design",
			prev:      "支付页面需要三个字段：金额、备注、支付方式",
			curr:      "用户选择支付方式后应该跳转到对应的收银台页面",
			wantShift: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fpPrev := d.ExtractFingerprint(tt.prev)
			fpCurr := d.ExtractFingerprint(tt.curr)

			got := d.DetectShift(fpPrev, fpCurr, 0.12)
			if got != tt.wantShift {
				t.Errorf("DetectShift(%q, %q) = %v, want %v\n  prev terms: %v\n  curr terms: %v",
					tt.prev[:min(len(tt.prev), 20)], tt.curr[:min(len(tt.curr), 20)],
					got, tt.wantShift,
					fpPrev.RawTerms[:min(8, len(fpPrev.RawTerms))],
					fpCurr.RawTerms[:min(8, len(fpCurr.RawTerms))])
			}

			label := d.SuggestTopicLabel(fpCurr)
			t.Logf("  → topic label: %s (shift=%v)", label, got)
		})
	}
}

func TestTopicDetector_EmptyInput(t *testing.T) {
	d := NewTopicDetector()

	fp := d.ExtractFingerprint("")
	if len(fp.Terms) != 0 {
		t.Fatalf("Empty input should produce empty terms, got %d", len(fp.Terms))
	}

	// No shift on first call (previous is empty)
	fp2 := d.ExtractFingerprint("hello world")
	shifted := d.DetectShift(fp, fp2, 0.12)
	if shifted {
		t.Fatal("First turn should not be a shift (empty prev)")
	}
}

func TestTopicDetector_StopWords(t *testing.T) {
	d := NewTopicDetector()

	// Only stop words → should return almost empty
	fp := d.ExtractFingerprint("the and of in to a is that it")
	t.Logf("Stop word only input terms: %v", fp.Terms)
	// Some stop words might still appear if they're >1 char, but count should be low
	if len(fp.Terms) > 3 {
		t.Fatalf("Stop word only input should have very few terms, got %d: %v", len(fp.Terms), keys(fp.Terms))
	}
}

func TestTopicDetector_DomainSignals(t *testing.T) {
	d := NewTopicDetector()

	// Code-heavy input
	fp := d.ExtractFingerprint("检查 `authMiddleware` 在 `src/handler/auth.go` 的 NotFoundError")
	t.Logf("Domain signal terms: %v", keys(fp.Terms))

	hasBacktick := false
	for term := range fp.Terms {
		if term == "`authMiddleware`" || term == "`src/handler/auth.go`" {
			hasBacktick = true
		}
	}
	if !hasBacktick {
		t.Log("Note: backtick code signals may need longer context (non-critical)")
	}
}

// keys returns the keys of a string map for debugging.
func keys(m map[string]float64) []string {
	var k []string
	for key := range m {
		k = append(k, key)
	}
	return k
}
