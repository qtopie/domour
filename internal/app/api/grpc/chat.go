package grpc

import (
	"context"

	chatpb "github.com/qtopie/domour/gen/assistant/chat"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/infra/llm"
	providerruntime "github.com/qtopie/domour/internal/infra/llm/runtime"
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
		Attachments: llm.AttachmentsFromProto(req.GetAttachments()),
		EditorContext: editorContextFromProto(req.GetEditorContext()),
	}

	err = s.app.Chat(ctx, motorReq, req.GetProvider(), req.GetModel(), func(event shared.MotorStreamEvent) error {
		typ := chatpb.ChunkType_CHUNK_TEXT
		if event.Type != 0 {
			typ = chatpb.ChunkType(event.Type)
		}
		resp := &chatpb.ChatResponse{
			SessionId: sessionID,
			Seq:       req.GetSeq(),
			Type:      typ,
			Content:   event.Content,
			Done:      event.Done,
			Meta:      mergeChatMeta(stream.Context(), sessionID, event.Meta, event.Stage),
		}
		if event.Thinking != nil {
			resp.Thinking = &chatpb.ThinkingDetail{
				Engine:    event.Thinking.Engine,
				Stage:     event.Thinking.Stage,
				ElapsedMs: event.Thinking.ElapsedMs,
			}
		}
		if event.Collaboration != nil {
			resp.Collaboration = &chatpb.CollaborationDetail{
				FromNode:    event.Collaboration.FromNode,
				ToNode:      event.Collaboration.ToNode,
				EventType:   event.Collaboration.EventType,
				Description: event.Collaboration.Description,
			}
		}
		if event.ToolCall != nil {
			resp.ToolCall = &chatpb.ToolCallDetail{
				ToolName:    event.ToolCall.ToolName,
				ToolId:      event.ToolCall.ToolID,
				Status:      event.ToolCall.Status,
				Arguments:   event.ToolCall.Arguments,
				Observation: event.ToolCall.Observation,
				DurationMs:  event.ToolCall.DurationMs,
			}
		}
		return stream.Send(resp)
	})
	if err != nil {
		s.logError(stream.Context(), "Chat", sessionID, err)
		return err
	}

	return nil
}

func mergeChatMeta(ctx context.Context, sessionID string, meta map[string]string, stage string) map[string]string {
	modeVal := "balanced"
	if rmeta := providerruntime.RequestMetadataFromContext(ctx); rmeta.Mode != "" {
		modeVal = rmeta.Mode
	}
	out := map[string]string{
		"entry":     "chat",
		"mode":      modeVal,
		"stage":     stage,
		"sessionId": sessionID,
		"traceId":   getTraceID(ctx),
	}
	for k, v := range meta {
		out[k] = v
	}
	return out
}

func editorContextFromProto(ec *chatpb.EditorContext) *shared.EditorContext {
	if ec == nil {
		return nil
	}
	pinned := ec.GetPinnedFiles()
	if len(pinned) == 0 {
		return nil
	}
	files := make([]shared.PinnedFile, 0, len(pinned))
	for _, pf := range pinned {
		files = append(files, shared.PinnedFile{
			Path:     pf.GetPath(),
			Content:  pf.GetContent(),
			Language: pf.GetLanguage(),
		})
	}
	return &shared.EditorContext{PinnedFiles: files}
}
