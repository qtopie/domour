// Package bootstrap handles the initialization, dependency injection, and 
// application lifecycle management for the Domour agent runtime.
package bootstrap

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/qtopie/domour/ark/session"
	domourskill "github.com/qtopie/domour/ark/skill"
	infraspi "github.com/qtopie/domour/ark/spi/infra"
	runtimespi "github.com/qtopie/domour/ark/spi/runtime"
	"github.com/qtopie/domour/ark/telemetry"
	domourtool "github.com/qtopie/domour/ark/tool"
	"github.com/qtopie/domour/internal/app/assistant"
	"github.com/qtopie/domour/internal/config"
	"github.com/qtopie/domour/internal/infra/eventbus"
	"google.golang.org/grpc"
)

// Run initializes the application by loading configuration, assembling 
// bionic components, and starting all background neural event loops.
// It blocks until the context is canceled or a critical error occurs.
func Run(ctx context.Context, opts ...Option) error {
	cfg, err := config.LoadDomourConfig()
	if err != nil {
		// Let NewApp handle the error or use defaults if cfg is nil
	}

	// Load skills from configured skills directories.
	// Multiple directories can be separated by commas.
	if cfg.SkillsDir != "" {
		dirs := strings.Split(cfg.SkillsDir, ",")
		for i, d := range dirs {
			trimmed := strings.TrimSpace(d)
			dirs[i] = trimmed
			_ = os.MkdirAll(trimmed, 0755)
		}
		if loaded, err := domourskill.LoadFromDirs(dirs...); err != nil {
			log.Printf("Warning: failed to load skills: %v\n", err)
		} else if len(loaded) > 0 {
			log.Printf("Loaded %d skill(s) from %d dir(s)\n", len(loaded), len(dirs))
		}
	}

	// Load MCP tools from configured MCP directories.
	// Multiple directories can be separated by commas.
	if cfg.MCPDir != "" {
		dirs := strings.Split(cfg.MCPDir, ",")
		for i, d := range dirs {
			trimmed := strings.TrimSpace(d)
			dirs[i] = trimmed
			_ = os.MkdirAll(trimmed, 0755)
		}
		if loaded, err := domourtool.LoadFromDirs(dirs...); err != nil {
			log.Printf("Warning: failed to load tools: %v\n", err)
		} else if len(loaded) > 0 {
			log.Printf("Loaded %d tool(s) from %d dir(s)\n", len(loaded), len(dirs))
		}
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

	var o options
	for _, opt := range opts {
		opt(&o)
	}

	appOpts := []assistant.AppOption{}
	if o.store != nil {
		appOpts = append(appOpts, assistant.WithStore(o.store))
	}
	if o.orchestrator != nil {
		appOpts = append(appOpts, assistant.WithOrchestrator(o.orchestrator))
	}
	if o.eventBus != nil {
		appOpts = append(appOpts, assistant.WithEventBus(o.eventBus))
	}
	if o.router != nil {
		appOpts = append(appOpts, assistant.WithRouter(o.router))
	}

	app, err := assistant.NewApp(&cfg, appOpts...)
	if err != nil {
		return fmt.Errorf("failed to create app: %w", err)
	}

	if o.grpcServer != nil {
		// If using shared gRPC server, we only need to start background loops (HTTP, engine)
		return app.RunBackground(ctx)
	}

	return app.Run(ctx)
}

// Option defines a functional option for the Run method.
type Option func(*options)

type options struct {
	grpcServer   *grpc.Server
	store        session.SessionStore
	orchestrator infraspi.AgentOrchestrator
	eventBus     eventbus.EventBus
	router       runtimespi.Router
}

// WithGRPCServer allows passing an existing gRPC server to reuse.
func WithGRPCServer(s *grpc.Server) Option {
	return func(o *options) {
		o.grpcServer = s
	}
}

// WithStore allows passing a custom session store/manager.
func WithStore(store session.SessionStore) Option {
	return func(o *options) {
		o.store = store
	}
}

// WithOrchestrator allows passing a custom agent workflow orchestrator.
func WithOrchestrator(orch infraspi.AgentOrchestrator) Option {
	return func(o *options) {
		o.orchestrator = orch
	}
}

// WithEventBus allows passing a custom event bus.
func WithEventBus(eb eventbus.EventBus) Option {
	return func(o *options) {
		o.eventBus = eb
	}
}

// WithRouter allows passing a custom request router.
func WithRouter(r runtimespi.Router) Option {
	return func(o *options) {
		o.router = r
	}
}

// RegisterAssistantServices initializes the assistant and registers its gRPC services
// onto the provided server. This allows embedding Domour into a host process.
func RegisterAssistantServices(s *grpc.Server, opts ...Option) error {
	cfg, _ := config.LoadDomourConfig()
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	var appOpts []assistant.AppOption
	if o.store != nil {
		appOpts = append(appOpts, assistant.WithStore(o.store))
	}
	if o.orchestrator != nil {
		appOpts = append(appOpts, assistant.WithOrchestrator(o.orchestrator))
	}
	if o.eventBus != nil {
		appOpts = append(appOpts, assistant.WithEventBus(o.eventBus))
	}
	if o.router != nil {
		appOpts = append(appOpts, assistant.WithRouter(o.router))
	}
	app, err := assistant.NewApp(&cfg, appOpts...)
	if err != nil {
		return err
	}
	return app.RegisterGRPC(s)
}
