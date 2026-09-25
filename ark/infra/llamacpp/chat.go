package llamacpp

import (
	"strings"
	"unicode/utf8"
)

// DefaultChatStops defines standard stop sequences for ChatML.
var DefaultChatStops = []string{
	"<|im_end|>",
	"<|endoftext|>",
	"<|im_start|>",
}

// FormatChatML converts a slice of ChatMessage into standard ChatML prompt format.
func FormatChatML(messages []ChatMessage) string {
	var sb strings.Builder
	for _, m := range messages {
		role := strings.TrimSpace(m.Role)
		if role == "" {
			role = "user"
		}
		sb.WriteString("<|im_start|>")
		sb.WriteString(role)
		sb.WriteString("\n")
		sb.WriteString(m.Content)
		sb.WriteString("<|im_end|>\n")
	}
	// Prompt assistant turn
	sb.WriteString("<|im_start|>assistant\n")
	return sb.String()
}

// UTF8StreamBuffer handles incomplete multi-byte UTF-8 sequences across streaming chunks.
type UTF8StreamBuffer struct {
	buf []byte
}

// NewUTF8StreamBuffer creates an initialized UTF-8 streaming buffer.
func NewUTF8StreamBuffer() *UTF8StreamBuffer {
	return &UTF8StreamBuffer{
		buf: make([]byte, 0, 128),
	}
}

// Feed appends incoming bytes and flushes all complete UTF-8 code points.
func (u *UTF8StreamBuffer) Feed(chunk []byte) string {
	u.buf = append(u.buf, chunk...)
	if len(u.buf) == 0 {
		return ""
	}

	validLen := 0
	i := 0
	for i < len(u.buf) {
		r, size := utf8.DecodeRune(u.buf[i:])
		if r == utf8.RuneError && size == 1 {
			// Incomplete UTF-8 sequence at the end of buffer
			if !utf8.FullRune(u.buf[i:]) {
				break
			}
		}
		validLen = i + size
		i += size
	}

	if validLen == 0 {
		return ""
	}

	out := string(u.buf[:validLen])
	u.buf = append([]byte(nil), u.buf[validLen:]...)
	return out
}

// Flush returns any remaining bytes as string.
func (u *UTF8StreamBuffer) Flush() string {
	if len(u.buf) == 0 {
		return ""
	}
	out := string(u.buf)
	u.buf = nil
	return out
}

// StopDetector checks for stop word matches in a streaming text token flow.
type StopDetector struct {
	stops []string
	buf   string
}

// NewStopDetector initializes a detector with the given stop words.
func NewStopDetector(stops []string) *StopDetector {
	clean := make([]string, 0, len(stops))
	for _, s := range stops {
		if s != "" {
			clean = append(clean, s)
		}
	}
	return &StopDetector{stops: clean}
}

// Process processes incoming text.
// Returns:
// - emit: text safe to emit to the caller.
// - stopped: true if a stop word has been encountered.
func (s *StopDetector) Process(incoming string) (emit string, stopped bool) {
	s.buf += incoming

	// Check if any stop word is present in s.buf
	for _, stop := range s.stops {
		if idx := strings.Index(s.buf, stop); idx >= 0 {
			emit = s.buf[:idx]
			s.buf = ""
			return emit, true
		}
	}

	// Check if s.buf ends with a prefix of any stop word
	maxPrefixLen := 0
	for _, stop := range s.stops {
		for l := 1; l < len(stop) && l <= len(s.buf); l++ {
			if strings.HasSuffix(s.buf, stop[:l]) {
				if l > maxPrefixLen {
					maxPrefixLen = l
				}
			}
		}
	}

	if maxPrefixLen > 0 {
		safeLen := len(s.buf) - maxPrefixLen
		emit = s.buf[:safeLen]
		s.buf = s.buf[safeLen:]
		return emit, false
	}

	emit = s.buf
	s.buf = ""
	return emit, false
}

// FlushRemaining returns any residual buffer when generation terminates without hitting a stop sequence.
func (s *StopDetector) FlushRemaining() string {
	res := s.buf
	s.buf = ""
	return res
}
