package llamacpp

import (
	"context"
	"sync"
)

type defaultEngine struct {
	cfg    Config
	worker *worker
	mu     sync.RWMutex
	closed bool
}

// NewEngine creates a new Engine instance with the given configuration.
func NewEngine(cfg Config) (Engine, error) {
	runner, err := newModelRunner(cfg)
	if err != nil {
		return nil, err
	}

	w := newWorker(runner, 64)
	return &defaultEngine{
		cfg:    cfg,
		worker: w,
	}, nil
}

func (e *defaultEngine) ForwardLogits(ctx context.Context, req ForwardRequest) (*ForwardResponse, error) {
	e.mu.RLock()
	if e.closed {
		e.mu.RUnlock()
		return nil, ErrBackendClosed
	}
	e.mu.RUnlock()

	respCh := make(chan fwdResult, 1)
	job := inferenceJob{
		ctx:       ctx,
		isForward: true,
		fwdReq:    req,
		fwdRespCh: respCh,
	}

	if err := e.worker.submit(job); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-respCh:
		return res.resp, res.err
	}
}

func (e *defaultEngine) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	e.mu.RLock()
	if e.closed {
		e.mu.RUnlock()
		return nil, ErrBackendClosed
	}
	e.mu.RUnlock()

	respCh := make(chan genResult, 1)
	job := inferenceJob{
		ctx:       ctx,
		isForward: false,
		genReq:    req,
		genRespCh: respCh,
	}

	if err := e.worker.submit(job); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-respCh:
		return res.resp, res.err
	}
}

func (e *defaultEngine) Stream(ctx context.Context, req GenerateRequest) (<-chan TokenChunk, error) {
	e.mu.RLock()
	if e.closed {
		e.mu.RUnlock()
		return nil, ErrBackendClosed
	}
	e.mu.RUnlock()

	streamCh := make(chan TokenChunk, 32)
	job := inferenceJob{
		ctx:       ctx,
		isForward: false,
		isStream:  true,
		genReq:    req,
		streamCh:  streamCh,
	}

	if err := e.worker.submit(job); err != nil {
		close(streamCh)
		return nil, err
	}

	return streamCh, nil
}

func (e *defaultEngine) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if len(req.Messages) == 0 {
		return nil, ErrEmptyInput
	}
	prompt := FormatChatML(req.Messages)
	stops := append([]string{}, req.Stop...)
	stops = append(stops, DefaultChatStops...)

	genResp, err := e.Generate(ctx, GenerateRequest{
		Prompt:      prompt,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stop:        stops,
	})
	if err != nil {
		return nil, err
	}

	return &ChatResponse{
		Message: ChatMessage{
			Role:    "assistant",
			Content: genResp.Content,
		},
		PromptTokens:     genResp.PromptTokens,
		CompletionTokens: genResp.CompletionTokens,
		FinishReason:     genResp.FinishReason,
	}, nil
}

func (e *defaultEngine) ChatStream(ctx context.Context, req ChatRequest) (<-chan TokenChunk, error) {
	if len(req.Messages) == 0 {
		return nil, ErrEmptyInput
	}
	prompt := FormatChatML(req.Messages)
	stops := append([]string{}, req.Stop...)
	stops = append(stops, DefaultChatStops...)

	return e.Stream(ctx, GenerateRequest{
		Prompt:      prompt,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stop:        stops,
	})
}

func (e *defaultEngine) IsReady() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return !e.closed && e.worker != nil
}

func (e *defaultEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	return e.worker.close()
}
