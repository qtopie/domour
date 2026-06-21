package shared

import (
	"encoding/json"
	"errors"
	"strings"
)

type UserRequest struct {
	SessionId string `json:"-"`
	Seq       int32  `json:"-"`
	Message   string
	FrontPart string
	BackPart  string
	Filename  string
	Workspace string
	History   []Message `json:"history,omitempty"`
}

type ChunkData struct {
	ID      string
	Content string
	IsLast  bool
}

type Message struct {
	Role     string `json:"role"` // "user" or "assistant"
	Content  string `json:"content"`
	Time     int64  `json:"time"`
	Seq      int32  `json:"seq,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

type BrainChatRequest struct {
	SessionID     string
	Seq           int32
	Workspace     string
	Message       string
	Filename      string
	FrontPart     string
	BackPart      string
	Attachments   []BrainAttachment
	Interception  *ChatInterception
	History       []Message
	MemorySummary string
	Provider      string
	Model         string
}

type BrainDiagramRequest struct {
	Workspace     string
	Message       string
	Filename      string
	FrontPart     string
	BackPart      string
	Attachments   []BrainAttachment
	History       []Message
	MemorySummary string
	Provider      string
	Model         string
}

type BrainCopilotRequest struct {
	Workspace     string
	Message       string
	Filename      string
	CodeBefore    string
	CodeAfter     string
	CursorOffset  int32
	Attachments   []BrainAttachment
	History       []Message
	MemorySummary string
	Provider      string
	Model         string
}

type BrainAutopilotRequest struct {
	Workspace     string
	Goal          string
	Constraints   []string
	MaxSteps      int32
	Attachments   []BrainAttachment
	History       []Message
	MemorySummary string
	Provider      string
	Model         string
}

type BrainTextResponse struct {
	Content  string
	Provider string
	Model    string
}

type BrainDiagramResponse struct {
	Summary  string
	Route    string
	Format   string
	Title    string
	Diagram  string
	Provider string
	Model    string
}

type BrainStreamEvent struct {
	Type     string
	Content  string
	Summary  string
	Route    string
	Format   string
	Title    string
	Diagram  string
	Provider string
	Model    string
	Err      error
}

type MotorChatRequest struct {
	SessionID   string
	Seq         int32
	Workspace   string
	Message     string
	Filename    string
	FrontPart   string
	BackPart    string
	Attachments []BrainAttachment
	History     []Message
}

type MotorAutopilotRequest struct {
	SessionID    string
	Seq          int32
	Workspace    string
	Goal         string
	Constraints  []string
	MaxSteps     int32
	HistoryCount int
}

type MotorCopilotRequest struct {
	SessionID    string
	Seq          int32
	Workspace    string
	Message      string
	Filename     string
	CodeBefore   string
	CodeAfter    string
	CursorOffset int32
	History      []Message
	Mode         string
}

type MotorAutopilotResponse struct {
	Status string
	Result string
	Meta   map[string]string
}

type MotorStreamEvent struct {
	Stage         string
	Type          int32 // maps to ChunkType (e.g. 1 for text, 2 for thinking, etc.)
	Content       string
	Done          bool
	Meta          map[string]string
	Err           error

	// Structured details
	Thinking      *ThinkingDetail
	Collaboration *CollaborationDetail
	ToolCall      *ToolCallDetail
}

func (e MotorStreamEvent) MarshalJSON() ([]byte, error) {
	type Alias MotorStreamEvent
	var errStr string
	if e.Err != nil {
		errStr = e.Err.Error()
	}
	return json.Marshal(&struct {
		Alias
		Err string `json:"Err,omitempty"`
	}{
		Alias: Alias(e),
		Err:   errStr,
	})
}

func (e *MotorStreamEvent) UnmarshalJSON(data []byte) error {
	type Alias MotorStreamEvent
	aux := &struct {
		*Alias
		Err string `json:"Err,omitempty"`
	}{
		Alias: (*Alias)(e),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.Err != "" {
		e.Err = errors.New(aux.Err)
	}
	return nil
}


type ThinkingDetail struct {
	Engine    string
	Stage     string
	ElapsedMs int64
}

type CollaborationDetail struct {
	FromNode    string
	ToNode      string
	EventType   string
	Description string
}

type ToolCallDetail struct {
	ToolName    string
	ToolID      string
	Status      string
	Arguments   string
	Observation string
	DurationMs  int64
}


type BrainControl struct {
	Type    string
	Content string
	Meta    map[string]string
}

type BrainAttachment struct {
	ID         string         `json:"id,omitempty"`
	Filename   string         `json:"filename,omitempty"`
	MIMEType   string         `json:"mime_type,omitempty"`
	URL        string         `json:"url,omitempty"`
	DataBase64 string         `json:"data_base64,omitempty"`
	SizeBytes  int64          `json:"size_bytes,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type ChatInterception struct {
	Source     string   `json:"source,omitempty"`
	Summary    string   `json:"summary,omitempty"`
	OCRText    string   `json:"ocr_text,omitempty"`
	KeyFacts   []string `json:"key_facts,omitempty"`
	Confidence float64  `json:"confidence,omitempty"` // 0.0 to 1.0
}

type SessionBridge struct {
	BrainOut chan BrainStreamEvent
	MotorOut chan MotorStreamEvent
	Control  chan BrainControl
}

func NewSessionBridge() *SessionBridge {
	return &SessionBridge{
		BrainOut: make(chan BrainStreamEvent, 8),
		MotorOut: make(chan MotorStreamEvent, 8),
		Control:  make(chan BrainControl, 4),
	}
}

const DefaultSessionID = "default-session"

func StripCodeFence(content string) string {
	import_strings := "strings"
	_ = import_strings
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "```") {
		return content
	}

	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return content
	}
	lines = lines[1:]
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func WantsOCRTask(message string) bool {
	text := strings.ToLower(strings.TrimSpace(message))
	if text == "" {
		return false
	}
	for _, marker := range []string{
		"ocr", "extract text", "text extraction", "transcribe", "read the image", "scan text",
		"识别文字", "识别图片中的文字", "提取文字", "提取文本", "图片文字", "图中文字", "文字识别", "ocr识别", "转文字", "读图",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
