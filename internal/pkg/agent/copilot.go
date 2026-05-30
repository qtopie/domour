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
	s.logCall(stream.Context(), "Copilot", sessionID)

	ctx := withRuntimeMetadata(stream.Context(), sessionID, req.GetWorkspace())
	sess, _ := s.getSession(ctx, sessionID)

	// Check provider readiness
	brainClient, err := s.brain.GetClient(ctx, "copilot")
	if err == nil {
		if ready, readyErr := brainClient.IsReady(ctx); !ready || readyErr != nil {
			err = readyErr
			if err == nil {
				err = fmt.Errorf("provider %s is not ready", brainClient.Provider())
			}
			s.logError(stream.Context(), "Copilot.Readiness", sessionID, err)
			return err
		}
	}

	userMessage := strings.TrimSpace(req.GetMessage())
	if userMessage == "" {
		userMessage = "Please describe the change you want to make."
	}
	_ = s.appendHistory(ctx, sessionID, "user", userMessage)

	mode := resolveCopilotMode(userMessage)
	brainCtx, brainCancel := context.WithCancel(ctx)
	defer brainCancel()

	brainReq := BrainCopilotRequest{
		Workspace:     req.GetWorkspace(),
		Message:       userMessage,
		Filename:      req.GetFilename(),
		CodeBefore:    req.GetCodeBefore(),
		CodeAfter:     req.GetCodeAfter(),
		CursorOffset:  req.GetCursorOffset(),
		Attachments:   attachmentsFromProto(req.GetAttachments()),
		History:       sess.History,
		MemorySummary: sess.MemorySummary,
		Provider:      req.GetProvider(),
		Model:         req.GetModel(),
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
		History:      sess.History,
		Mode:         mode,
	}, func(bridge *SessionBridge) {
		go s.streamCopilotBrainToBridge(brainCtx, brainCancel, brainReq, bridge)
	})
	if err != nil {
		s.logError(stream.Context(), "Copilot", sessionID, err)
		return err
	}

	var parts []string
	var lastProvider, lastModel string
	for event := range motorStream {
		if event.Err != nil {
			s.logError(stream.Context(), "Copilot", sessionID, event.Err)
			return event.Err
		}
		if strings.TrimSpace(event.Content) != "" {
			parts = append(parts, event.Content)
		}
		if event.Meta != nil {
			if p, ok := event.Meta["provider"]; ok && p != "" {
				lastProvider = p
			}
			if m, ok := event.Meta["model"]; ok && m != "" {
				lastModel = m
			}
		}
		if err := stream.Send(&copilotpb.CopilotResponse{
			SessionId: sessionID,
			Seq:       req.GetSeq(),
			Patch:     event.Content,
			Complete:  event.Done,
			Meta:      mergeCopilotMeta(stream.Context(), sessionID, event.Meta),
		}); err != nil {
			s.logError(stream.Context(), "Copilot", sessionID, err)
			return err
		}
	}

	_ = s.appendHistoryWithMeta(ctx, sessionID, "assistant", strings.Join(parts, "\n"), lastProvider, lastModel)
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

func mergeCopilotMeta(ctx context.Context, sessionID string, meta map[string]string) map[string]string {
	out := map[string]string{
		"entry":     "copilot",
		"mode":      "mvp",
		"sessionId": sessionID,
		"traceId":   getTraceID(ctx),
	}
	for k, v := range meta {
		out[k] = v
	}
	return out
}
