package agent

import (
	"context"
	"fmt"
	"strings"

	copilotpb "github.com/qtopie/domour/gen/assistant/copilot"
	"google.golang.org/grpc"
)

func (s *Server) Copilot(req *copilotpb.CopilotRequest, stream grpc.ServerStreamingServer[copilotpb.CopilotResponse]) error {
	sessionID := normalizeSessionID(req.GetSessionId())
	ctx := withRuntimeMetadata(stream.Context(), sessionID, req.GetWorkspace())
	history, _ := s.getHistory(ctx, sessionID)

	userMessage := strings.TrimSpace(req.GetMessage())
	if userMessage == "" {
		userMessage = "Please describe the change you want to make."
	}
	_ = s.appendHistory(ctx, sessionID, "user", userMessage)

	mode := resolveCopilotMode(userMessage)
	brainCtx, brainCancel := context.WithCancel(ctx)
	defer brainCancel()

	brainReq := BrainCopilotRequest{
		Workspace:    req.GetWorkspace(),
		Message:      userMessage,
		Filename:     req.GetFilename(),
		CodeBefore:   req.GetCodeBefore(),
		CodeAfter:    req.GetCodeAfter(),
		CursorOffset: req.GetCursorOffset(),
		History:      history,
	}
	motorStream, err := s.motor.Copilot(ctx, MotorCopilotRequest{
		SessionID:    sessionID,
		Seq:          req.GetSeq(),
		Workspace:    req.GetWorkspace(),
		Message:      userMessage,
		Filename:     req.GetFilename(),
		CodeBefore:   req.GetCodeBefore(),
		CodeAfter:    req.GetCodeAfter(),
		CursorOffset: req.GetCursorOffset(),
		History:      history,
		Mode:         mode,
	}, func(bridge *SessionBridge) {
		go s.streamCopilotBrainToBridge(brainCtx, brainCancel, brainReq, bridge)
	})
	if err != nil {
		return err
	}

	var parts []string
	for event := range motorStream {
		if event.Err != nil {
			return event.Err
		}
		if strings.TrimSpace(event.Content) != "" {
			parts = append(parts, event.Content)
		}
		if err := stream.Send(&copilotpb.CopilotResponse{
			SessionId: sessionID,
			Seq:       req.GetSeq(),
			Patch:     event.Content,
			Complete:  event.Done,
			Meta:      mergeCopilotMeta(event.Meta),
		}); err != nil {
			return err
		}
	}

	_ = s.appendHistory(ctx, sessionID, "assistant", strings.Join(parts, "\n"))
	return nil
}

func buildCopilotPrompt(userMessage, workspace, filename, before, after string, cursorOffset int32) string {
	parts := []string{strings.TrimSpace(userMessage)}
	if workspace := strings.TrimSpace(workspace); workspace != "" {
		parts = append(parts, "Workspace: "+workspace)
	}
	if filename := strings.TrimSpace(filename); filename != "" {
		parts = append(parts, "Target file: "+filename)
	}
	if before := strings.TrimSpace(before); before != "" {
		parts = append(parts, "Code before cursor:\n"+before)
	}
	if after := strings.TrimSpace(after); after != "" {
		parts = append(parts, "Code after cursor:\n"+after)
	}
	if cursorOffset > 0 {
		parts = append(parts, fmt.Sprintf("Cursor offset: %d", cursorOffset))
	}
	return strings.Join(parts, "\n\n")
}

func mergeCopilotMeta(meta map[string]string) map[string]string {
	out := map[string]string{
		"entry": "copilot",
		"mode":  "mvp",
	}
	for k, v := range meta {
		out[k] = v
	}
	return out
}
