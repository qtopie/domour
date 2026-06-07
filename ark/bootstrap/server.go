// Package bootstrap handles the initialization, dependency injection, and 
// application lifecycle management for the Domour agent runtime.
package bootstrap

import (
	"context"
	"fmt"
	"log"

	"github.com/qtopie/domour/ark/telemetry"
	"github.com/qtopie/domour/internal/app/assistant"
	"github.com/qtopie/domour/internal/config"
)

// Run initializes the application by loading configuration, assembling 
// bionic components, and starting all background neural event loops.
// It blocks until the context is canceled or a critical error occurs.
func Run(ctx context.Context) error {
	cfg, err := config.LoadDomourConfig()
	if err != nil {
		// Let NewApp handle the error or use defaults if cfg is nil
	}

	// Initialize Telemetry
	shutdown, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName:    cfg.TelemetryServiceName(),
		ServiceVersion: cfg.TelemetryServiceVersion(),
		Endpoint:       cfg.TelemetryEndpoint(),
		Sampler:        cfg.TelemetrySampler(),
		UseStdout:      cfg.TelemetryUseStdout(),
	})
	if err != nil {
		log.Printf("Warning: failed to setup telemetry: %v\n", err)
	} else if shutdown != nil {
		defer func() {
			if err := shutdown(context.Background()); err != nil {
				log.Printf("Error: failed to shutdown telemetry: %v\n", err)
			}
		}()
	}

	app, err := assistant.NewApp(&cfg)
	if err != nil {
		return fmt.Errorf("failed to create app: %w", err)
	}

	return app.Run(ctx)
}
