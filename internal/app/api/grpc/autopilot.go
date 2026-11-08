package grpc

import (
	"context"

	autopilotpb "github.com/qtopie/domour/gen/assistant/autopilot"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/infra/llm"
	providerruntime "github.com/qtopie/domour/internal/infra/llm/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func (s *Server) Autopilot(ctx context.Context, req *autopilotpb.AutopilotRequest) (*autopilotpb.AutopilotResponse, error) {
	sessionID := normalizeSessionID(req.GetSessionId())
	ctx = withRuntimeMetadata(ctx, sessionID, req.GetWorkspace())
	ctx, span := otel.Tracer("domour/grpc").Start(ctx, "domour.grpc.Autopilot", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	span.SetAttributes(
		attribute.String("session.id", sessionID),
		attribute.String("llm.provider", req.GetProvider()),
		attribute.String("llm.model", req.GetModel()),
	)

	s.logCall(ctx, "Autopilot", sessionID)

	unlock, err := s.app.Locker().Lock(ctx, sessionID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		s.logError(ctx, "Autopilot.Lock", sessionID, err)
		return nil, err
	}
	defer unlock()

	motorReq := shared.MotorAutopilotRequest{
		SessionID:   sessionID,
		Seq:         req.GetSeq(),
		Workspace:   req.GetWorkspace(),
		Goal:        req.GetGoal(),
		Constraints: req.GetConstraints(),
		MaxSteps:    req.GetMaxSteps(),
	}

	attachments := llm.AttachmentsFromProto(req.GetAttachments())

	resp, err := s.app.Autopilot(ctx, motorReq, req.GetProvider(), req.GetModel(), attachments)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		s.logError(ctx, "Autopilot", sessionID, err)
		return nil, err
	}

	return &autopilotpb.AutopilotResponse{
		SessionId: sessionID,
		Seq:       req.GetSeq(),
		Status:    resp.Status,
		Result:    resp.Result,
		Meta:      mergeAutopilotMeta(ctx, sessionID, resp.Meta),
	}, nil
}

func mergeAutopilotMeta(ctx context.Context, sessionID string, meta map[string]string) map[string]string {
	modeVal := "balanced"
	if rmeta := providerruntime.RequestMetadataFromContext(ctx); rmeta.Mode != "" {
		modeVal = rmeta.Mode
	}
	out := map[string]string{
		"entry":     "autopilot",
		"mode":      modeVal,
		"sessionId": sessionID,
		"traceId":   getTraceID(ctx),
	}
	for k, v := range meta {
		out[k] = v
	}
	return out
}
