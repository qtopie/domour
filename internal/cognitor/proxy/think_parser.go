package proxy

import (
	"strings"

	"github.com/qtopie/domour/internal/app/assistant/shared"
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

// NewThinkTagParser creates a new ThinkTagParser instance.
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
	brainClient *Client,
	yield func(event shared.MotorStreamEvent) error,
) error {
	var meta map[string]string
	if brainClient != nil {
		meta = map[string]string{"provider": brainClient.Provider(), "model": brainClient.Model()}
	}

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
						Type:    0, // CHUNK_TEXT
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
					Type:    0, // CHUNK_TEXT
					Content: content[:idx],
					Meta:    meta,
				}); err != nil {
					return err
				}
			}

			// Emit thinking-start marker
			p.isThinking = true
			var engine string
			if brainClient != nil {
				engine = brainClient.Provider()
			}
			if err := yield(shared.MotorStreamEvent{
				Stage: stage,
				Type:  1, // CHUNK_THINKING
				Thinking: &shared.ThinkingDetail{
					Engine: engine,
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
					var engine string
					if brainClient != nil {
						engine = brainClient.Provider()
					}
					if err := yield(shared.MotorStreamEvent{
						Stage:   stage,
						Type:    1, // CHUNK_THINKING
						Content: content[:safeEnd],
						Thinking: &shared.ThinkingDetail{
							Engine: engine,
							Stage:  "thought",
						},
						Meta: meta,
					}); err != nil {
						return err
					}
				}
				p.buf.WriteString(content[safeEnd:])
				break
			}

			// Thinking text before closing tag
			if idx > 0 {
				var engine string
				if brainClient != nil {
					engine = brainClient.Provider()
				}
				if err := yield(shared.MotorStreamEvent{
					Stage:   stage,
					Type:    1, // CHUNK_THINKING
					Content: content[:idx],
					Thinking: &shared.ThinkingDetail{
						Engine: engine,
						Stage:  "thought",
					},
					Meta: meta,
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

// Flush emits any remaining buffered text when stream ends.
func (p *ThinkTagParser) Flush(
	stage string,
	brainClient *Client,
	yield func(event shared.MotorStreamEvent) error,
) error {
	rem := p.buf.String()
	p.buf.Reset()
	if rem == "" {
		return nil
	}
	var meta map[string]string
	var engine string
	if brainClient != nil {
		meta = map[string]string{"provider": brainClient.Provider(), "model": brainClient.Model()}
		engine = brainClient.Provider()
	}
	chunkType := int32(0)
	var thinking *shared.ThinkingDetail
	if p.isThinking {
		chunkType = 1
		thinking = &shared.ThinkingDetail{
			Engine: engine,
			Stage:  "thought",
		}
	}
	return yield(shared.MotorStreamEvent{
		Stage:    stage,
		Type:     chunkType,
		Content:  rem,
		Thinking: thinking,
		Meta:     meta,
	})
}

// safeFlushEnd calculates how many bytes at the start of s can safely be flushed,
// preserving any trailing suffix that matches a prefix of tag.
func safeFlushEnd(s, tag string) int {
	maxMatch := 0
	for i := 1; i <= len(s) && i <= len(tag); i++ {
		if strings.HasSuffix(s, tag[:i]) {
			maxMatch = i
		}
	}
	return len(s) - maxMatch
}
