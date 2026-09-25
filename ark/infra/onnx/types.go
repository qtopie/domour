package onnx

import (
	"context"
	"errors"
)

var (
	// ErrBackendClosed is returned when operations are attempted on a closed ONNX engine.
	ErrBackendClosed = errors.New("onnx: backend is closed")
	// ErrInvalidBatch is returned when batch dimensions are mismatched or invalid.
	ErrInvalidBatch = errors.New("onnx: invalid batch input dimensions")
)

// Config defines initialization parameters for the ONNX decision engine.
type Config struct {
	ModelPath  string `json:"model_path"`
	NumThreads int    `json:"num_threads"`
	UseStub    bool   `json:"use_stub"`
}

// BatchInput is the input tensor batch passed to ONNX.
type BatchInput struct {
	BatchSize     int     `json:"batch_size"`
	SeqLen        int     `json:"seq_len"`
	MaxMarkers    int     `json:"max_markers"`
	InputIDs      []int64 `json:"input_ids"`      // [BatchSize * SeqLen]
	AttentionMask []int64 `json:"attention_mask"` // [BatchSize * SeqLen]
	MarkerPos     []int64 `json:"marker_pos"`     // [BatchSize * MaxMarkers]
	MarkerMask    []bool  `json:"marker_mask"`    // [BatchSize * MaxMarkers]
	QType         []int64 `json:"q_type"`         // [BatchSize] (0: choice, 1: score, 2: boolean)
}

// BatchOutput contains raw logits and action probabilities produced by the model heads.
type BatchOutput struct {
	Logits   []float32 `json:"logits"`    // [BatchSize * MaxMarkers]
	ActProbs []float32 `json:"act_probs"` // [BatchSize * 2]
}

// Engine defines the ONNX decision engine interface.
type Engine interface {
	Run(ctx context.Context, batch BatchInput) (BatchOutput, error)
	Close() error
}
