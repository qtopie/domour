package grpc

import (
	"context"

	chatpb "github.com/qtopie/domour/gen/assistant/chat"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/infra/llm"
	"google.golang.org/grpc"
)

func (s *Server) Chat(req *chatpb.ChatRequest, stream grpc.ServerStreamingServer[chatpb.ChatResponse]) error {
	sessionID := normalizeSessionID(req.GetSessionId())
	s.logCall(stream.Context(), "Chat", sessionID)

	ctx := withRuntimeMetadata(stream.Context(), sessionID, req.GetWorkspace())
	unlock, err := s.app.Locker().Lock(ctx, sessionID)
	if err != nil {
		s.logError(stream.Context(), "Chat.Lock", sessionID, err)
		return err
	}
	defer unlock()

	motorReq := shared.MotorChatRequest{
		SessionID:   sessionID,
		Seq:         req.GetSeq(),
		Workspace:   req.GetWorkspace(),
		Message:     req.GetMessage(),
		Filename:    req.GetFilename(),
		FrontPart:   req.GetFrontPart(),
		BackPart:    req.GetBackPart(),
		Attachments: llm.AttachmentsFromProto(req.GetAttachments()),
	}

	err = s.app.Chat(ctx, motorReq, req.GetProvider(), req.GetModel(), func(event shared.MotorStreamEvent) error {
		return stream.Send(&chatpb.ChatResponse{
			SessionId: sessionID,
			Seq:       req.GetSeq(),
			Content:   event.Content,
			Done:      event.Done,
			Meta:      mergeChatMeta(stream.Context(), sessionID, event.Meta, event.Stage),
		})
	})
	if err != nil {
		s.logError(stream.Context(), "Chat", sessionID, err)
		return err
	}

	return nil
}

func mergeChatMeta(ctx context.Context, sessionID string, meta map[string]string, stage string) map[string]string {
	out := map[string]string{
		"entry":     "chat",
		"mode":      "mvp",
		"stage":     stage,
		"sessionId": sessionID,
		"traceId":   getTraceID(ctx),
	}
	for k, v := range meta {
		out[k] = v
	}
	return out
}
