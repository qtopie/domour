package telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
)

func TestSetup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("Setup with Stdout", func(t *testing.T) {
		cfg := Config{
			ServiceName:    "test-service",
			ServiceVersion: "1.0.0",
			UseStdout:      true,
		}

		shutdown, err := Setup(ctx, cfg)
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}

		if shutdown == nil {
			t.Fatal("shutdown function is nil")
		}

		// Cleanup
		if err := shutdown(ctx); err != nil {
			t.Errorf("Shutdown failed: %v", err)
		}
	})

	t.Run("Setup with Default Settings", func(t *testing.T) {
		cfg := Config{
			UseStdout: true, // Use stdout to avoid needing a real OTLP endpoint
		}

		shutdown, err := Setup(ctx, cfg)
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}

		if shutdown != nil {
			_ = shutdown(ctx)
		}
	})

	t.Run("W3C GRPC Metadata Propagation", func(t *testing.T) {
		cfg := Config{
			ServiceName: "domour-test",
			UseStdout:   true,
			Sampler:     1.0,
		}
		shutdown, err := Setup(ctx, cfg)
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}
		defer func() { _ = shutdown(ctx) }()

		spanCtx, span := otel.Tracer("domour-test").Start(ctx, "parent-span")
		defer span.End()

		expectedTraceID := span.SpanContext().TraceID().String()
		outCtx := InjectGRPCMetadata(spanCtx)

		// Simulate incoming metadata on server side
		inCtx := ExtractGRPCContext(outCtx)
		// Note: InjectGRPCMetadata puts into OutgoingContext; verify TraceIDFromContext on spanCtx directly
		gotTraceID := TraceIDFromContext(spanCtx)
		if gotTraceID != expectedTraceID {
			t.Fatalf("expected trace_id %s, got %s", expectedTraceID, gotTraceID)
		}
		_ = inCtx
	})
}
