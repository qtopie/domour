package llamacpp

import (
	"fmt"
	"math"
	"strings"
	"sync"
)

type stubRunner struct {
	mu     sync.Mutex
	closed bool
}

func (s *stubRunner) Tokenize(prompt string) ([]int32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrBackendClosed
	}

	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return []int32{}, nil
	}

	parts := strings.Fields(trimmed)
	tokens := make([]int32, len(parts))
	for i, p := range parts {
		var h int32
		for _, b := range []byte(p) {
			h = h*31 + int32(b)
		}
		if h < 0 {
			h = -h
		}
		tokens[i] = (h % 32000) + 1
	}
	return tokens, nil
}

func (s *stubRunner) Forward(tokens []int32, markerIndices []int) (map[int][]float32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrBackendClosed
	}

	nTokens := len(tokens)
	result := make(map[int][]float32, len(markerIndices))

	for _, idx := range markerIndices {
		if idx < 0 || idx >= nTokens {
			return nil, fmt.Errorf("%w: index %d out of bounds [0, %d)", ErrInvalidMarkerIndex, idx, nTokens)
		}

		tokID := tokens[idx]
		const vocabSize = 64
		logits := make([]float32, vocabSize)
		for v := 0; v < vocabSize; v++ {
			diff := float64((int32(v) - (tokID % vocabSize)))
			val := float32(10.0 * math.Exp(-0.1*diff*diff))
			logits[v] = val
		}
		result[idx] = logits
	}

	return result, nil
}

func (s *stubRunner) SampleNext(tokens []int32, temp float32) (int32, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, "", ErrBackendClosed
	}

	step := len(tokens)
	nextTokenID := int32(100 + step)
	piece := fmt.Sprintf(" chunk%d", step)
	return nextTokenID, piece, nil
}

func (s *stubRunner) DecodeStep(token int32, pos int32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrBackendClosed
	}
	return nil
}

func (s *stubRunner) ClearKV() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrBackendClosed
	}
	return nil
}

func (s *stubRunner) TokenToPiece(token int32) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", ErrBackendClosed
	}
	return fmt.Sprintf(" tok_%d", token), nil
}

func (s *stubRunner) GetEOS() int32 {
	return 2
}

func (s *stubRunner) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}
