package onnx_test

import (
	"context"
	"sync"
	"testing"

	"github.com/qtopie/domour/ark/infra/onnx"
)

func TestEngine_Lifecycle(t *testing.T) {
	eng, err := onnx.NewEngine(onnx.Config{
		ModelPath: "test.onnx",
		UseStub:   true,
	})
	if err != nil {
		t.Fatalf("unexpected NewEngine error: %v", err)
	}

	ctx := context.Background()
	batch := onnx.BatchInput{
		BatchSize:  2,
		SeqLen:     32,
		MaxMarkers: 4,
		QType:      []int64{0, 2},
	}

	out, err := eng.Run(ctx, batch)
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}

	if len(out.Logits) != 8 {
		t.Fatalf("expected 8 logits, got %d", len(out.Logits))
	}
	if len(out.ActProbs) != 4 {
		t.Fatalf("expected 4 act_probs, got %d", len(out.ActProbs))
	}

	if err := eng.Close(); err != nil {
		t.Fatalf("unexpected Close error: %v", err)
	}

	// Post-close run should fail
	_, err = eng.Run(ctx, batch)
	if err == nil {
		t.Fatalf("expected error after close, got nil")
	}
}

func TestEngine_Concurrency(t *testing.T) {
	eng, err := onnx.NewEngine(onnx.Config{UseStub: true})
	if err != nil {
		t.Fatalf("unexpected NewEngine error: %v", err)
	}
	defer eng.Close()

	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			batch := onnx.BatchInput{
				BatchSize:  1,
				SeqLen:     16,
				MaxMarkers: 3,
				QType:      []int64{1},
			}
			out, err := eng.Run(ctx, batch)
			if err != nil {
				t.Errorf("concurrent Run error: %v", err)
			}
			if len(out.Logits) != 3 {
				t.Errorf("expected 3 logits, got %d", len(out.Logits))
			}
		}()
	}
	wg.Wait()
}
