package provider

import (
	"context"
)

// CLIConfig holds the configuration for a CLI-based LLM execution.
type CLIConfig struct {
	Command  string            // The command name or path, e.g. "gemini", "claude"
	Model    string            // The target model name
	ProxyURL string            // Optional proxy setting
	Env      map[string]string // Custom environment variables
	Debug    bool              // Debug mode
}

// CLIProvider defines the interface for executing LLMs via command-line tools.
type CLIProvider interface {
	// ProviderName returns the name of this provider (e.g. "gemini-cli")
	ProviderName() string

	// ModelName returns the model name being used
	ModelName() string

	// Generate runs the command-line LLM with the given prompt and options, returning the text output.
	Generate(ctx context.Context, prompt string, opts ...Option) (string, error)

	// IsAvailable checks if the CLI tool is available on the local system.
	IsAvailable(ctx context.Context) (bool, error)
}

// Options for CLI execution.
type Options struct {
	SystemPrompt string
	AssetPaths   []string // For multimodal inputs (e.g. image attachments)
}

type Option func(*Options)

func WithSystemPrompt(p string) Option {
	return func(o *Options) {
		o.SystemPrompt = p
	}
}

func WithAssetPaths(paths []string) Option {
	return func(o *Options) {
		o.AssetPaths = paths
	}
}
