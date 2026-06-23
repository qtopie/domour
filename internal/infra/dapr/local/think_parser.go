package local

import (
	"strings"

	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/cognitor/proxy"
)

// ThinkTagParser is a stateful parser that handles <think>...</think> blocks
// which may be split across multiple streaming chunks.
// It buffers partial tag characters to avoid emitting them as text prematurely.
type ThinkTagParser struct {
	// isThinking is true when we are inside a <think> block
	isThinking bool
	// buf holds characters that might be part of a tag but are not yet confirmed
	buf strings.Builder

	openTag  string // "<think>"
	closeTag string // "</think>"
}

func NewThinkTagParser() *ThinkTagParser {
	return &ThinkTagParser{
		openTag:  "<think>",
		closeTag: "</think>",
	}
}

// Feed processes a new chunk and calls yield for any complete text/thinking segments.
func (p *ThinkTagParser) Feed(
	chunk string,
	stage string,
	brainClient *proxy.Client,
	yield func(event shared.MotorStreamEvent) error,
) error {
	meta := map[string]string{"provider": brainClient.Provider(), "model": brainClient.Model()}

	p.buf.WriteString(chunk)
	content := p.buf.String()
	p.buf.Reset()

	for content != "" {
		if !p.isThinking {
			// Look for the start of <think>
			tag := p.openTag
			idx := strings.Index(content, tag)

			if idx == -1 {
				// No <think> found - but we might be at the start of a partial tag
				// e.g. content ends with "<thi" — we must not flush those chars yet
				safeEnd := safeFlushEnd(content, tag)
				if safeEnd > 0 {
					if err := yield(shared.MotorStreamEvent{
						Stage:   stage,
						Type:    1, // CHUNK_TEXT
						Content: content[:safeEnd],
						Meta:    meta,
					}); err != nil {
						return err
					}
				}
				// Keep the potential partial tag in buffer for next Feed call
				p.buf.WriteString(content[safeEnd:])
				break
			}

			// Text before the tag
			if idx > 0 {
				if err := yield(shared.MotorStreamEvent{
					Stage:   stage,
					Type:    1, // CHUNK_TEXT
					Content: content[:idx],
					Meta:    meta,
				}); err != nil {
					return err
				}
			}

			// Emit thinking-start marker
			p.isThinking = true
			if err := yield(shared.MotorStreamEvent{
				Stage: stage,
				Type:  2, // CHUNK_THINKING
				Thinking: &shared.ThinkingDetail{
					Engine: brainClient.Provider(),
					Stage:  "thought",
				},
				Meta: meta,
			}); err != nil {
				return err
			}
			content = content[idx+len(tag):]

		} else {
			// Inside <think> block - look for </think>
			tag := p.closeTag
			idx := strings.Index(content, tag)

			if idx == -1 {
				safeEnd := safeFlushEnd(content, tag)
				if safeEnd > 0 {
					if err := yield(shared.MotorStreamEvent{
						Stage:   stage,
						Type:    2, // CHUNK_THINKING
						Content: content[:safeEnd],
						Meta:    meta,
					}); err != nil {
						return err
					}
				}
				p.buf.WriteString(content[safeEnd:])
				break
			}

			// Thinking content before </think>
			if idx > 0 {
				if err := yield(shared.MotorStreamEvent{
					Stage:   stage,
					Type:    2, // CHUNK_THINKING
					Content: content[:idx],
					Meta:    meta,
				}); err != nil {
					return err
				}
			}
			p.isThinking = false
			content = content[idx+len(tag):]
		}
	}

	return nil
}

// Flush emits any remaining buffered content as text (or thinking if still inside a block).
// Call this after the last chunk.
func (p *ThinkTagParser) Flush(
	stage string,
	brainClient *proxy.Client,
	yield func(event shared.MotorStreamEvent) error,
) error {
	remaining := p.buf.String()
	if remaining == "" {
		return nil
	}
	p.buf.Reset()

	meta := map[string]string{"provider": brainClient.Provider(), "model": brainClient.Model()}
	var chunkType int32 = 1 // CHUNK_TEXT
	if p.isThinking {
		chunkType = 2 // CHUNK_THINKING
	}
	return yield(shared.MotorStreamEvent{
		Stage:   stage,
		Type:    chunkType,
		Content: remaining,
		Meta:    meta,
	})
}

// safeFlushEnd returns the index up to which content can be safely flushed
// without risk of cutting into a partial `tag`. Characters near the end that
// could be the beginning of `tag` are held back in the buffer.
func safeFlushEnd(content, tag string) int {
	maxHold := len(tag) - 1
	if maxHold <= 0 || len(content) <= maxHold {
		// Hold everything if it could all be a partial tag prefix
		for i := len(content); i >= 1; i-- {
			if strings.HasPrefix(tag, content[len(content)-i:]) {
				return len(content) - i
			}
		}
		return len(content)
	}

	// Walk backwards to find the longest suffix of `content` that is a prefix of `tag`
	for holdBack := maxHold; holdBack >= 1; holdBack-- {
		suffix := content[len(content)-holdBack:]
		if strings.HasPrefix(tag, suffix) {
			return len(content) - holdBack
		}
	}
	return len(content)
}
