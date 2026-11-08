package grpc

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/qtopie/domour/ark/telemetry"
	appconfig "github.com/qtopie/domour/internal/config"
)

func (s *Server) logCall(ctx context.Context, method string, sessionID string) {
	msg := fmt.Sprintf("calling %s", method)
	s.writeLog(ctx, "info", msg, method, sessionID)
}

func (s *Server) logError(ctx context.Context, method string, sessionID string, err error) {
	s.writeLog(ctx, "error", err.Error(), method, sessionID)
}

func (s *Server) logDebug(ctx context.Context, method string, sessionID string, msg string) {
	s.writeLog(ctx, "debug", msg, method, sessionID)
}

func (s *Server) writeLog(ctx context.Context, level, msg, method, sessionID string) {
	cfg, _ := appconfig.LoadDomourConfig()

	if level == "debug" && !cfg.IsDebug() {
		return
	}

	traceID := getTraceID(ctx)
	attrs := []any{
		slog.String("service", "domour"),
		slog.String("component", "grpc"),
		slog.String("method", method),
		slog.String("session_id", sessionID),
		slog.String("trace_id", traceID),
	}

	switch level {
	case "error":
		slog.ErrorContext(ctx, msg, attrs...)
	case "debug":
		slog.DebugContext(ctx, msg, attrs...)
	default:
		slog.InfoContext(ctx, msg, attrs...)
	}
}

func getTraceID(ctx context.Context) string {
	return telemetry.TraceIDFromContext(ctx)
}
