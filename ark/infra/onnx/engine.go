package onnx

import (
	"context"
	"sync"
)

type defaultEngine struct {
	cfg    Config
	runner modelRunner
	mu     sync.RWMutex
	closed bool
}

// NewEngine creates a new ONNX Engine with the given configuration.
func NewEngine(cfg Config) (Engine, error) {
	runner, err := newModelRunner(cfg)
	if err != nil {
		return nil, err
	}

	return &defaultEngine{
		cfg:    cfg,
		runner: runner,
	}, nil
}

// Run executes a forward pass on the input batch.
func (e *defaultEngine) Run(ctx context.Context, batch BatchInput) (BatchOutput, error) {
	e.mu.RLock()
	if e.closed {
		e.mu.RUnlock()
		return BatchOutput{}, ErrBackendClosed
	}
	e.mu.RUnlock()

	select {
	case <-ctx.Done():
		return BatchOutput{}, ctx.Err()
	default:
	}

	return e.runner.Run(batch)
}

// Close terminates the engine and releases native resources.
func (e *defaultEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true
	return e.runner.Close()
}
