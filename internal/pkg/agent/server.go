package agent

import (
	"context"
	"os"
	"strings"
	"time"

	autopilotpb "github.com/qtopie/domour/gen/assistant/autopilot"
	chatpb "github.com/qtopie/domour/gen/assistant/chat"
	copilotpb "github.com/qtopie/domour/gen/assistant/copilot"
	"github.com/qtopie/domour/internal/pkg/copilot/shared"
	providerruntime "github.com/qtopie/domour/internal/provider/runtime"
	"github.com/qtopie/domour/internal/session"
)

const defaultSessionID = "default-session"

// Server is the minimal built-in agent server behind chat/copilot/autopilot.
type Server struct {
	autopilotpb.UnimplementedAutopilotServiceServer
	chatpb.UnimplementedChatServiceServer
	copilotpb.UnimplementedCopilotServiceServer

	store session.Store
	brain BrainClient
	motor MotorClient
}

func NewServer(store session.Store) (*Server, error) {
	if store == nil {
		store = session.NewMemoryStore()
	}

	brain, err := newConfiguredBrainClient()
	if err != nil {
		return nil, err
	}
	motorClient, err := newConfiguredMotorClient()
	if err != nil {
		return nil, err
	}

	return &Server{
		store: store,
		brain: brain,
		motor: motorClient,
	}, nil
}

func (s *Server) appendHistory(ctx context.Context, sessionID, role, content string) error {
	if s.store == nil {
		return nil
	}
	return s.store.AppendHistory(ctx, sessionID, shared.Message{
		Role:    role,
		Content: content,
		Time:    time.Now().Unix(),
	})
}

func (s *Server) getHistory(ctx context.Context, sessionID string) ([]shared.Message, error) {
	if s.store == nil {
		return nil, nil
	}
	return s.store.GetHistory(ctx, sessionID)
}

func normalizeSessionID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return defaultSessionID
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
	return providerruntime.WithRequestMetadata(ctx, providerruntime.RequestMetadata{
		SessionID: sessionID,
		Workspace: strings.TrimSpace(workspace),
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

func (s *Server) streamBrainToBridge(ctx context.Context, cancel context.CancelFunc, req BrainChatRequest, bridge *SessionBridge) {
	defer close(bridge.BrainOut)

	req = waitForInitialChatInterception(ctx, req, bridge)
	brainStream, err := s.brain.StreamChat(ctx, req)
	if err != nil {
		bridge.BrainOut <- BrainStreamEvent{Type: "error", Err: err}
		return
	}

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() != context.Canceled {
				bridge.BrainOut <- BrainStreamEvent{Type: "error", Err: ctx.Err()}
			}
			return
		case control := <-bridge.Control:
			switch control.Type {
			case "stop", "refuse":
				cancel()
				return
			}
		case event, ok := <-brainStream:
			if !ok {
				return
			}
			bridge.BrainOut <- event
			if event.Err != nil {
				return
			}
		}
	}
}

func (s *Server) streamMotorToBridge(ctx context.Context, req MotorChatRequest, bridge *SessionBridge) {
	_ = s.motor.StreamChat(ctx, req, bridge)
}

func (s *Server) streamAutopilotBrainToBridge(ctx context.Context, cancel context.CancelFunc, req BrainAutopilotRequest, bridge *SessionBridge) {
	defer close(bridge.BrainOut)

	brainStream, err := s.brain.StreamAutopilot(ctx, req)
	if err != nil {
		bridge.BrainOut <- BrainStreamEvent{Type: "error", Err: err}
		return
	}

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() != context.Canceled {
				bridge.BrainOut <- BrainStreamEvent{Type: "error", Err: ctx.Err()}
			}
			return
		case control := <-bridge.Control:
			switch control.Type {
			case "stop", "refuse":
				cancel()
				return
			}
		case event, ok := <-brainStream:
			if !ok {
				return
			}
			bridge.BrainOut <- event
			if event.Err != nil {
				return
			}
		}
	}
}

func (s *Server) streamCopilotBrainToBridge(ctx context.Context, cancel context.CancelFunc, req BrainCopilotRequest, bridge *SessionBridge) {
	defer close(bridge.BrainOut)

	brainStream, err := s.brain.StreamCopilot(ctx, req)
	if err != nil {
		bridge.BrainOut <- BrainStreamEvent{Type: "error", Err: err}
		return
	}

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() != context.Canceled {
				bridge.BrainOut <- BrainStreamEvent{Type: "error", Err: ctx.Err()}
			}
			return
		case control := <-bridge.Control:
			switch control.Type {
			case "stop", "refuse":
				cancel()
				return
			}
		case event, ok := <-brainStream:
			if !ok {
				return
			}
			bridge.BrainOut <- event
			if event.Err != nil {
				return
			}
		}
	}
}
