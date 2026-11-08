package grpc

import (
	"context"

	copilotpb "github.com/qtopie/domour/gen/assistant/copilot"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/infra/llm"
	providerruntime "github.com/qtopie/domour/internal/infra/llm/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

func (s *Server) Copilot(req *copilotpb.CopilotRequest, stream grpc.ServerStreamingServer[copilotpb.CopilotResponse]) error {
	sessionID := normalizeSessionID(req.GetSessionId())
	ctx := withRuntimeMetadata(stream.Context(), sessionID, req.GetWorkspace())
	ctx, span := otel.Tracer("domour/grpc").Start(ctx, "domour.grpc.Copilot", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	span.SetAttributes(
		attribute.String("session.id", sessionID),
		attribute.String("llm.provider", req.GetProvider()),
		attribute.String("llm.model", req.GetModel()),
	)

	s.logCall(ctx, "Copilot", sessionID)

	unlock, err := s.app.Locker().Lock(ctx, sessionID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		s.logError(ctx, "Copilot.Lock", sessionID, err)
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
			Meta:      mergeCopilotMeta(ctx, sessionID, event.Meta),
		})
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		s.logError(ctx, "Copilot", sessionID, err)
		return err
	}

	return nil
}

func mergeCopilotMeta(ctx context.Context, sessionID string, meta map[string]string) map[string]string {
	modeVal := "balanced"
	if rmeta := providerruntime.RequestMetadataFromContext(ctx); rmeta.Mode != "" {
		modeVal = rmeta.Mode
	}
	out := map[string]string{
		"entry":     "copilot",
		"mode":      modeVal,
		"sessionId": sessionID,
		"traceId":   getTraceID(ctx),
	}
	for k, v := range meta {
		out[k] = v
	}
	return out
}
