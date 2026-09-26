//go:build llamacpp

package llamacpp

/*
#cgo CFLAGS: -I/home/qtopierw/workspace/spacemit/llama.cpp/include -I/home/qtopierw/workspace/spacemit/llama.cpp/ggml/include -I/root/llama-installed/include -I/root/llama.cpp/include -I/root/llama.cpp/ggml/include
#cgo LDFLAGS: -L/home/qtopierw/workspace/spacemit/llama.cpp/build-x86/bin -L/root/llama-installed/lib -lllama -Wl,-rpath,/home/qtopierw/workspace/spacemit/llama.cpp/build-x86/bin -Wl,-rpath,/root/llama-installed/lib
#include "bridge.h"
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"sync"
	"unsafe"
)

type cgoRunner struct {
	handle *C.domour_llama_t
	vocab  int32
	kvPos  int32
	mu     sync.Mutex
	closed bool
}

func newModelRunner(cfg Config) (modelRunner, error) {
	if cfg.UseStub {
		return &stubRunner{}, nil
	}

	C.domour_llama_backend_init()

	cPath := C.CString(cfg.ModelPath)
	defer C.free(unsafe.Pointer(cPath))

	handle := C.domour_llama_load(cPath, C.uint32_t(cfg.NCtx), C.int32_t(cfg.NThreads))
	if handle == nil {
		C.domour_llama_backend_free()
		return nil, fmt.Errorf("llamacpp: failed to load model %q", cfg.ModelPath)
	}

	vocabSize := int32(C.domour_llama_get_vocab_size(handle))

	return &cgoRunner{
		handle: handle,
		vocab:  vocabSize,
	}, nil
}

func (c *cgoRunner) Tokenize(prompt string) ([]int32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrBackendClosed
	}

	cPrompt := C.CString(prompt)
	defer C.free(unsafe.Pointer(cPrompt))
	promptLen := C.int32_t(len(prompt))

	maxTokens := promptLen + 16
	if maxTokens < 32 {
		maxTokens = 32
	}
	tokens := make([]int32, maxTokens)

	n := C.domour_llama_tokenize(c.handle, cPrompt, promptLen, (*C.int32_t)(&tokens[0]), C.int32_t(maxTokens))
	if n < 0 {
		needed := -int(n)
		tokens = make([]int32, needed)
		n = C.domour_llama_tokenize(c.handle, cPrompt, promptLen, (*C.int32_t)(&tokens[0]), C.int32_t(needed))
		if n < 0 {
			return nil, fmt.Errorf("llamacpp: tokenize failed with code %d", int(n))
		}
	}

	return tokens[:n], nil
}

func (c *cgoRunner) Forward(tokens []int32, markerIndices []int) (map[int][]float32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrBackendClosed
	}

	nTokens := len(tokens)
	for _, idx := range markerIndices {
		if idx < 0 || idx >= nTokens {
			return nil, fmt.Errorf("%w: index %d out of bounds [0, %d)", ErrInvalidMarkerIndex, idx, nTokens)
		}
	}

	if nTokens == 0 {
		return make(map[int][]float32), nil
	}

	C.domour_llama_kv_clear(c.handle)
	c.kvPos = 0

	cTokens := (*C.int32_t)(unsafe.Pointer(&tokens[0]))
	cMarkers := make([]int32, len(markerIndices))
	for i, m := range markerIndices {
		cMarkers[i] = int32(m)
	}
	var pMarkers *C.int32_t
	if len(cMarkers) > 0 {
		pMarkers = (*C.int32_t)(unsafe.Pointer(&cMarkers[0]))
	}

	res := C.domour_llama_forward(c.handle, cTokens, C.int32_t(nTokens), pMarkers, C.int32_t(len(cMarkers)))
	if res != 0 {
		return nil, fmt.Errorf("llamacpp: decode forward failed with code %d", int(res))
	}

	result := make(map[int][]float32, len(markerIndices))
	for _, idx := range markerIndices {
		logitsPtr := C.domour_llama_get_logits(c.handle, C.int32_t(idx))
		if logitsPtr == nil {
			return nil, fmt.Errorf("llamacpp: failed to get logits for marker %d", idx)
		}
		logitsSlice := unsafe.Slice((*float32)(unsafe.Pointer(logitsPtr)), c.vocab)
		copied := make([]float32, c.vocab)
		copy(copied, logitsSlice)
		result[idx] = copied
	}

	return result, nil
}

func (c *cgoRunner) SampleNext(tokens []int32, temp float32) (int32, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, "", ErrBackendClosed
	}

	nTokens := len(tokens)
	if nTokens == 0 {
		return 0, "", fmt.Errorf("llamacpp: empty tokens for SampleNext")
	}

	var logitsPtr *C.float
	if c.kvPos == 0 {
		// First step (Prefill): evaluate all prompt tokens
		C.domour_llama_kv_clear(c.handle)
		lastIdx := nTokens - 1
		cTokens := (*C.int32_t)(unsafe.Pointer(&tokens[0]))
		marker := int32(lastIdx)

		res := C.domour_llama_forward(c.handle, cTokens, C.int32_t(nTokens), (*C.int32_t)(&marker), 1)
		if res != 0 {
			return 0, "", fmt.Errorf("llamacpp: decode forward failed with code %d", int(res))
		}
		c.kvPos = int32(nTokens)
		logitsPtr = C.domour_llama_get_logits(c.handle, C.int32_t(lastIdx))
	} else {
		// Incremental step: decode only the last token at position c.kvPos
		lastToken := tokens[nTokens-1]
		res := C.domour_llama_decode_step(c.handle, C.int32_t(lastToken), C.int32_t(c.kvPos))
		if res != 0 {
			return 0, "", fmt.Errorf("llamacpp: decode step failed with code %d", int(res))
		}
		c.kvPos++
		// Logits for single-token batch is at index 0
		logitsPtr = C.domour_llama_get_logits(c.handle, 0)
	}

	if logitsPtr == nil {
		return 0, "", fmt.Errorf("llamacpp: failed to get logits")
	}

	logits := unsafe.Slice((*float32)(unsafe.Pointer(logitsPtr)), c.vocab)
	bestIdx := int32(0)
	bestVal := logits[0]
	for i := int32(1); i < c.vocab; i++ {
		if logits[i] > bestVal {
			bestVal = logits[i]
			bestIdx = i
		}
	}

	buf := make([]byte, 256)
	pBuf := (*C.char)(unsafe.Pointer(&buf[0]))
	n := C.domour_llama_token_to_piece(c.handle, C.int32_t(bestIdx), pBuf, C.int32_t(len(buf)))
	piece := ""
	if n > 0 {
		piece = string(buf[:n])
	}

	return bestIdx, piece, nil
}

func (c *cgoRunner) DecodeStep(token int32, pos int32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrBackendClosed
	}

	res := C.domour_llama_decode_step(c.handle, C.int32_t(token), C.int32_t(pos))
	if res != 0 {
		return fmt.Errorf("llamacpp: decode step failed with code %d", int(res))
	}
	return nil
}

func (c *cgoRunner) ClearKV() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrBackendClosed
	}

	C.domour_llama_kv_clear(c.handle)
	c.kvPos = 0
	return nil
}

func (c *cgoRunner) TokenToPiece(token int32) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return "", ErrBackendClosed
	}

	buf := make([]byte, 256)
	pBuf := (*C.char)(unsafe.Pointer(&buf[0]))
	n := C.domour_llama_token_to_piece(c.handle, C.int32_t(token), pBuf, C.int32_t(len(buf)))
	if n <= 0 {
		return "", nil
	}
	return string(buf[:n]), nil
}

func (c *cgoRunner) GetEOS() int32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.handle == nil {
		return -1
	}
	return int32(C.domour_llama_get_eos(c.handle))
}

func (c *cgoRunner) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true

	if c.handle != nil {
		C.domour_llama_free(c.handle)
		c.handle = nil
	}
	C.domour_llama_backend_free()
	return nil
}
