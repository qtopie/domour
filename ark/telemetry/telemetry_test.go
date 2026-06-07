package telemetry

import (
	"context"
	"testing"
	"time"
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
			shutdown(ctx)
		}
	})
}
