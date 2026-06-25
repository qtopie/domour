package grpc

import (
	"context"
	"os"
	"strings"

	autopilotpb "github.com/qtopie/domour/gen/assistant/autopilot"
	chatpb "github.com/qtopie/domour/gen/assistant/chat"
	copilotpb "github.com/qtopie/domour/gen/assistant/copilot"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/pkg/bionic/session"
	providerruntime "github.com/qtopie/domour/internal/infra/llm/runtime"
	"google.golang.org/grpc/metadata"
)

// AssistantService defines the interface for assistant application logic
// needed by the gRPC API handlers.
type AssistantService interface {
	Locker() session.Locker
	Chat(ctx context.Context, req shared.MotorChatRequest, provider, model string, onEvent func(shared.MotorStreamEvent) error) error
	Copilot(ctx context.Context, req shared.MotorCopilotRequest, provider, model string, attachments []shared.BrainAttachment, onEvent func(shared.MotorStreamEvent) error) error
	Autopilot(ctx context.Context, req shared.MotorAutopilotRequest, provider, model string, attachments []shared.BrainAttachment) (shared.MotorAutopilotResponse, error)
	ListModels(ctx context.Context) ([]*chatpb.ModelInfo, error)
}

// Server is the minimal built-in agent server behind chat/copilot/autopilot.
type Server struct {
	autopilotpb.UnimplementedAutopilotServiceServer
	chatpb.UnimplementedChatServiceServer
	copilotpb.UnimplementedCopilotServiceServer

	app AssistantService
}

func NewServer(app AssistantService) (*Server, error) {
	return &Server{
		app: app,
	}, nil
}

func normalizeSessionID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return shared.DefaultSessionID
	}
	return sessionID
}

func firstNonEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func withRuntimeMetadata(ctx context.Context, sessionID, workspace string) context.Context {
	mode := "balanced"
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if modes := md.Get("x-domour-mode"); len(modes) > 0 {
			mode = modes[0]
		}
	}
	return providerruntime.WithRequestMetadata(ctx, providerruntime.RequestMetadata{
		SessionID: sessionID,
		Workspace: strings.TrimSpace(workspace),
		Mode:      mode,
	})
}

func resolveCopilotMode(message string) string {
	lower := strings.ToLower(strings.TrimSpace(message))
	switch {
	case strings.Contains(lower, "[active]"), strings.Contains(lower, "积极模式"), strings.Contains(lower, "/active"):
		return "active"
	case strings.Contains(lower, "[normal]"), strings.Contains(lower, "普通模式"), strings.Contains(lower, "/normal"):
		return "normal"
	}

	mode := strings.ToLower(strings.TrimSpace(os.Getenv("DOMOUR_COPILOT_MODE")))
	if mode == "active" {
		return "active"
	}
	return "normal"
}
