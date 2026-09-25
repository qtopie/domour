package onnx

import (
	"sync"
)

type stubRunner struct {
	mu     sync.Mutex
	cfg    Config
	closed bool
}

func newStubRunner(cfg Config) *stubRunner {
	return &stubRunner{
		cfg: cfg,
	}
}

func (s *stubRunner) Run(batch BatchInput) (BatchOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return BatchOutput{}, ErrBackendClosed
	}

	n := batch.BatchSize
	k := batch.MaxMarkers
	if n <= 0 || k <= 0 {
		return BatchOutput{}, ErrInvalidBatch
	}

	logits := make([]float32, n*k)
	actProbs := make([]float32, n*2)

	for i := 0; i < n; i++ {
		var qType int64
		if i < len(batch.QType) {
			qType = batch.QType[i]
		}

		offset := i * k
		switch qType {
		case 0: // Choice: candidate 0 gets highest logit
			if k > 0 {
				logits[offset+0] = 5.0
			}
			for j := 1; j < k; j++ {
				logits[offset+j] = 1.0 / float32(j+1)
			}
		case 1: // Score: level 2 gets highest logit
			for j := 0; j < k; j++ {
				if j == 2 {
					logits[offset+j] = 4.0
				} else {
					logits[offset+j] = 0.5
				}
			}
		case 2: // Boolean: index 1 (True) gets high logit
			if k > 0 {
				logits[offset+0] = 0.1
			}
			if k > 1 {
				logits[offset+1] = 3.5
			}
		default:
			for j := 0; j < k; j++ {
				logits[offset+j] = 1.0
			}
		}

		actProbs[i*2] = 0.95
		actProbs[i*2+1] = 0.05
	}

	return BatchOutput{
		Logits:   logits,
		ActProbs: actProbs,
	}, nil
}

func (s *stubRunner) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}
