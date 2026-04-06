package provider

import (
	"bufio"
	"context"
	"os/exec"
)

type CLIProvider struct {
	command string
	args    []string
}

func NewCLIProvider(command string, args ...string) *CLIProvider {
	return &CLIProvider{
		command: command,
		args:    args,
	}
}

func (p *CLIProvider) Name() string {
	return "cli"
}

func (p *CLIProvider) Generate(ctx context.Context, req GenerateRequest) (<-chan Chunk, error) {
	out := make(chan Chunk)
	
	// Example: execute a CLI command with user input as argument or stdin
	// For simplicity, we'll pass the message as an argument for now.
	// In a real implementation, we might use stdin/stdout for streaming.
	
	args := append(p.args, req.Content)
	cmd := exec.CommandContext(ctx, p.command, args...)
	
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	
	go func() {
		defer close(out)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			out <- Chunk{Text: scanner.Text() + "\n"}
		}
		
		if err := cmd.Wait(); err != nil {
			out <- Chunk{Err: err}
		}
		out <- Chunk{Done: true}
	}()
	
	return out, nil
}
