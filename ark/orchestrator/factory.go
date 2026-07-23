package orchestrator

import (
	"fmt"

	"github.com/qtopie/domour/internal/engine"
	"github.com/qtopie/domour/internal/infra/dapr"
)

// Mode represents the execution engine type for running agents.
type Mode string

const (
	ModeEinoNative Mode = "eino"
	ModeDurable    Mode = "durable"
)

// Config configures the AgentRunner factory.
type Config struct {
	Mode       Mode
	Engine     engine.Engine
	DaprClient *dapr.DurableAgentClient
}

// NewRunner creates a unified AgentRunner based on the provided configuration.
func NewRunner(cfg Config) (AgentRunner, error) {
	switch cfg.Mode {
	case ModeEinoNative:
		if cfg.Engine == nil {
			return nil, fmt.Errorf("engine cannot be nil for EinoNative mode")
		}
		return NewEinoNativeRunner(cfg.Engine), nil
	case ModeDurable:
		if cfg.DaprClient == nil {
			return nil, fmt.Errorf("daprClient cannot be nil for Durable mode")
		}
		return NewDurableAgentRunner(cfg.DaprClient), nil
	default:
		if cfg.Engine != nil {
			return NewEinoNativeRunner(cfg.Engine), nil
		}
		return nil, fmt.Errorf("unsupported orchestrator mode: %s", cfg.Mode)
	}
}
