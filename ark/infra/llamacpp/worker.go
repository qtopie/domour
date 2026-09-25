package llamacpp

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type fwdResult struct {
	resp *ForwardResponse
	err  error
}

type genResult struct {
	resp *GenerateResponse
	err  error
}

type inferenceJob struct {
	ctx       context.Context
	isForward bool
	isStream  bool
	fwdReq    ForwardRequest
	genReq    GenerateRequest
	streamCh  chan<- TokenChunk
	fwdRespCh chan<- fwdResult
	genRespCh chan<- genResult
}

type worker struct {
	runner modelRunner
	jobs   chan inferenceJob
	done   chan struct{}
	once   sync.Once
	closed bool
	mu     sync.RWMutex
}

func newWorker(runner modelRunner, queueSize int) *worker {
	if queueSize <= 0 {
		queueSize = 64
	}
	w := &worker{
		runner: runner,
		jobs:   make(chan inferenceJob, queueSize),
		done:   make(chan struct{}),
	}
	go w.loop()
	return w
}

func (w *worker) loop() {
	for {
		select {
		case <-w.done:
			return
		case job := <-w.jobs:
			if job.isForward {
				w.handleForward(job)
			} else {
				w.handleGenerate(job)
			}
		}
	}
}

func (w *worker) handleForward(job inferenceJob) {
	if err := job.ctx.Err(); err != nil {
		if job.fwdRespCh != nil {
			job.fwdRespCh <- fwdResult{err: err}
		}
		return
	}

	tokens := job.fwdReq.Tokens
	if len(tokens) == 0 {
		if strings.TrimSpace(job.fwdReq.Prompt) == "" {
			if job.fwdRespCh != nil {
				job.fwdRespCh <- fwdResult{err: ErrEmptyInput}
			}
			return
		}
		var err error
		tokens, err = w.runner.Tokenize(job.fwdReq.Prompt)
		if err != nil {
			if job.fwdRespCh != nil {
				job.fwdRespCh <- fwdResult{err: fmt.Errorf("llamacpp: tokenize failed: %w", err)}
			}
			return
		}
	}

	nTokens := len(tokens)
	for _, idx := range job.fwdReq.MarkerIndices {
		if idx < 0 || idx >= nTokens {
			if job.fwdRespCh != nil {
				job.fwdRespCh <- fwdResult{err: fmt.Errorf("%w: marker index %d outside token range [0, %d)", ErrInvalidMarkerIndex, idx, nTokens)}
			}
			return
		}
	}

	logitsMap, err := w.runner.Forward(tokens, job.fwdReq.MarkerIndices)
	if err != nil {
		if job.fwdRespCh != nil {
			job.fwdRespCh <- fwdResult{err: err}
		}
		return
	}

	if job.fwdRespCh != nil {
		job.fwdRespCh <- fwdResult{
			resp: &ForwardResponse{
				TokensCount: nTokens,
				Logits:      logitsMap,
			},
		}
	}
}

func (w *worker) handleGenerate(job inferenceJob) {
	if err := job.ctx.Err(); err != nil {
		if job.streamCh != nil {
			close(job.streamCh)
		}
		if job.genRespCh != nil {
			job.genRespCh <- genResult{err: err}
		}
		return
	}

	if strings.TrimSpace(job.genReq.Prompt) == "" {
		if job.streamCh != nil {
			close(job.streamCh)
		}
		if job.genRespCh != nil {
			job.genRespCh <- genResult{err: ErrEmptyInput}
		}
		return
	}

	tokens, err := w.runner.Tokenize(job.genReq.Prompt)
	if err != nil {
		if job.streamCh != nil {
			close(job.streamCh)
		}
		if job.genRespCh != nil {
			job.genRespCh <- genResult{err: fmt.Errorf("llamacpp: tokenize failed: %w", err)}
		}
		return
	}

	maxTokens := job.genReq.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 128
	}

	_ = w.runner.ClearKV()
	defer func() { _ = w.runner.ClearKV() }()

	detector := NewStopDetector(job.genReq.Stop)
	utf8Buf := NewUTF8StreamBuffer()
	eosToken := w.runner.GetEOS()

	var emittedText strings.Builder
	promptTokensCount := len(tokens)
	completionTokensCount := 0
	finishReason := "length"

	for i := 0; i < maxTokens; i++ {
		if err := job.ctx.Err(); err != nil {
			finishReason = "cancelled"
			break
		}

		nextToken, piece, sampleErr := w.runner.SampleNext(tokens, job.genReq.Temperature)
		if sampleErr != nil {
			if job.streamCh != nil {
				job.streamCh <- TokenChunk{Err: sampleErr}
			}
			break
		}

		if eosToken >= 0 && nextToken == eosToken {
			finishReason = "stop"
			break
		}

		tokens = append(tokens, nextToken)
		completionTokensCount++

		cleanText := utf8Buf.Feed([]byte(piece))
		if cleanText != "" {
			safeToEmit, stopped := detector.Process(cleanText)
			if safeToEmit != "" {
				emittedText.WriteString(safeToEmit)
				if job.streamCh != nil {
					select {
					case <-job.ctx.Done():
						finishReason = "cancelled"
					case job.streamCh <- TokenChunk{Text: safeToEmit, TokenID: nextToken, Done: false}:
					}
				}
			}
			if stopped {
				finishReason = "stop"
				break
			}
		}
	}

	// Flush remaining from UTF-8 buffer and Stop detector
	if remainingBytes := utf8Buf.Flush(); remainingBytes != "" {
		safeToEmit, stopped := detector.Process(remainingBytes)
		if safeToEmit != "" {
			emittedText.WriteString(safeToEmit)
			if job.streamCh != nil {
				job.streamCh <- TokenChunk{Text: safeToEmit, Done: false}
			}
		}
		if stopped {
			finishReason = "stop"
		}
	}

	if finishReason != "stop" {
		if rem := detector.FlushRemaining(); rem != "" {
			emittedText.WriteString(rem)
			if job.streamCh != nil {
				job.streamCh <- TokenChunk{Text: rem, Done: false}
			}
		}
	}

	if job.streamCh != nil {
		job.streamCh <- TokenChunk{Done: true}
		close(job.streamCh)
	}

	if job.genRespCh != nil {
		job.genRespCh <- genResult{
			resp: &GenerateResponse{
				Content:          emittedText.String(),
				PromptTokens:     promptTokensCount,
				CompletionTokens: completionTokensCount,
				FinishReason:     finishReason,
			},
		}
	}
}

func (w *worker) submit(job inferenceJob) error {
	w.mu.RLock()
	if w.closed {
		w.mu.RUnlock()
		return ErrBackendClosed
	}
	w.mu.RUnlock()

	select {
	case <-job.ctx.Done():
		return job.ctx.Err()
	case w.jobs <- job:
		return nil
	}
}

func (w *worker) close() error {
	w.once.Do(func() {
		w.mu.Lock()
		w.closed = true
		w.mu.Unlock()

		close(w.done)
		if w.runner != nil {
			_ = w.runner.Close()
		}
	})
	return nil
}
