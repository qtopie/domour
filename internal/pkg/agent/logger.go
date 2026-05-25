package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	appconfig "github.com/qtopie/domour/internal/app/config"
	"go.opentelemetry.io/otel/trace"
)

func (s *Server) logCall(ctx context.Context, method string, sessionID string) {
	msg := fmt.Sprintf("calling %s", method)
	s.writeLog(ctx, "info", msg, method, sessionID)
}

func (s *Server) logError(ctx context.Context, method string, sessionID string, err error) {
	s.writeLog(ctx, "error", err.Error(), method, sessionID)
}

func (s *Server) writeLog(ctx context.Context, level, msg, method, sessionID string) {
	cfg, _ := appconfig.LoadDomourConfig()
	traceID := getTraceID(ctx)
	now := time.Now().Format(time.RFC3339)
	scope := "cosmos.domour"

	if cfg.IsLogAsJSON() {
		logEntry := map[string]interface{}{
			"time":       now,
			"level":      level,
			"msg":        msg,
			"scope":      scope,
			"type":       "log",
			"trace_id":   traceID,
			"method":     method,
			"session_id": sessionID,
		}
		data, _ := json.Marshal(logEntry)
		fmt.Fprintln(os.Stderr, string(data))
	} else {
		fmt.Fprintf(os.Stderr, "time=\"%s\" level=%s msg=\"%s\" scope=%s type=log trace_id=%s method=%s session_id=%s\n",
			now, level, msg, scope, traceID, method, sessionID)
	}
}

func getTraceID(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return "00000000000000000000000000000000"
	}
	return spanCtx.TraceID().String()
}
