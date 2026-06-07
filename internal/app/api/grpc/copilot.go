package grpc

import (
	"context"

	copilotpb "github.com/qtopie/domour/gen/assistant/copilot"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/infra/llm"
	"google.golang.org/grpc"
)

func (s *Server) Copilot(req *copilotpb.CopilotRequest, stream grpc.ServerStreamingServer[copilotpb.CopilotResponse]) error {
	sessionID := normalizeSessionID(req.GetSessionId())
	s.logCall(stream.Context(), "Copilot", sessionID)

	ctx := withRuntimeMetadata(stream.Context(), sessionID, req.GetWorkspace())
	unlock, err := s.app.Locker().Lock(ctx, sessionID)
	if err != nil {
		s.logError(stream.Context(), "Copilot.Lock", sessionID, err)
		return err
	}
	defer unlock()

	mode := resolveCopilotMode(req.GetMessage())

	motorReq := shared.MotorCopilotRequest{
		SessionID:    sessionID,
		Seq:          req.GetSeq(),
		Workspace:    req.GetWorkspace(),
		Message:      req.GetMessage(),
		Filename:     req.GetFilename(),
		CodeBefore:   req.GetCodeBefore(),
		CodeAfter:    req.GetCodeAfter(),
		CursorOffset: req.GetCursorOffset(),
		Mode:         mode,
	}

	attachments := llm.AttachmentsFromProto(req.GetAttachments())

	err = s.app.Copilot(ctx, motorReq, req.GetProvider(), req.GetModel(), attachments, func(event shared.MotorStreamEvent) error {
		return stream.Send(&copilotpb.CopilotResponse{
			SessionId: sessionID,
			Seq:       req.GetSeq(),
			Patch:     event.Content,
			Complete:  event.Done,
			Meta:      mergeCopilotMeta(stream.Context(), sessionID, event.Meta),
		})
	})
	if err != nil {
		s.logError(stream.Context(), "Copilot", sessionID, err)
		return err
	}

	return nil
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
