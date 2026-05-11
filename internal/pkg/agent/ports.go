package agent

import (
	"context"

	"github.com/qtopie/domour/internal/pkg/copilot/shared"
	"github.com/qtopie/domour/internal/pkg/motor"
)

type BrainClient interface {
	StreamChat(ctx context.Context, req BrainChatRequest) (<-chan BrainStreamEvent, error)
	StreamAutopilot(ctx context.Context, req BrainAutopilotRequest) (<-chan BrainStreamEvent, error)
	StreamCopilot(ctx context.Context, req BrainCopilotRequest) (<-chan BrainStreamEvent, error)
	ChatReply(ctx context.Context, req BrainChatRequest) (BrainTextResponse, error)
	PlanDiagram(ctx context.Context, req BrainDiagramRequest) (BrainDiagramResponse, error)
	Copilot(ctx context.Context, req BrainCopilotRequest) (BrainTextResponse, error)
	Autopilot(ctx context.Context, req BrainAutopilotRequest) (BrainTextResponse, error)
}

type MotorClient interface {
	StreamChat(ctx context.Context, req MotorChatRequest, bridge *SessionBridge) error
	Autopilot(ctx context.Context, req MotorAutopilotRequest, startBrain func(*SessionBridge)) (MotorAutopilotResponse, error)
	Copilot(ctx context.Context, req MotorCopilotRequest, startBrain func(*SessionBridge)) (<-chan MotorStreamEvent, error)
	Execute(ctx context.Context, command motor.Command) (motor.Result, error)
}

type BrainChatRequest struct {
	SessionID    string
	Seq          int32
	Workspace    string
	Message      string
	Filename     string
	FrontPart    string
	BackPart     string
	Attachments  []BrainAttachment
	Interception *ChatInterception
	History      []shared.Message
}

type BrainDiagramRequest struct {
	Workspace   string
	Message     string
	Filename    string
	FrontPart   string
	BackPart    string
	Attachments []BrainAttachment
	History     []shared.Message
}

type BrainCopilotRequest struct {
	Workspace    string
	Message      string
	Filename     string
	CodeBefore   string
	CodeAfter    string
	CursorOffset int32
	Attachments  []BrainAttachment
	History      []shared.Message
}

type BrainAutopilotRequest struct {
	Workspace   string
	Goal        string
	Constraints []string
	MaxSteps    int32
	Attachments []BrainAttachment
	History     []shared.Message
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
	History     []shared.Message
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
	History      []shared.Message
	Mode         string
}

type MotorAutopilotResponse struct {
	Status string
	Result string
	Meta   map[string]string
}

type MotorStreamEvent struct {
	Stage   string
	Content string
	Done    bool
	Meta    map[string]string
	Err     error
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

func newSessionBridge() *SessionBridge {
	return &SessionBridge{
		BrainOut: make(chan BrainStreamEvent, 8),
		MotorOut: make(chan MotorStreamEvent, 8),
		Control:  make(chan BrainControl, 4),
	}
}
