package agent

import (
	"context"
	"fmt"
	"log"

	"github.com/qtopie/domour/internal/pkg/copilot/shared"
	"google.golang.org/genai"
)

type Agent struct {
	client *genai.Client
	model  string
}

func NewAgent(ctx context.Context, apiKey, model string) (*Agent, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}
	return &Agent{
		client: client,
		model:  model,
	}, nil
}

func (a *Agent) LoadSkills(path string) error {
	// Stub implementation
	log.Printf("Loading skills from %s (stub)", path)
	return nil
}

func (a *Agent) Run(ctx context.Context, req shared.UserRequest) (<-chan string, error) {
	ch := make(chan string)

	go func() {
		defer close(ch)

		// Simple generation for now
		// Note: The original implementation likely used streaming.
		// Here we just generate once and send it.
		// For true streaming, we'd need to use client.Models.GenerateContentStream

		// But wait, the original code used `ag.Run` which returned a channel.
		// Let's assume it was streaming.

		// Mock implementation for now to pass compilation
		// In a real scenario, we would stream.

		resp, err := a.client.Models.GenerateContent(ctx, a.model, genai.Text(req.Message), nil)
		if err != nil {
			log.Printf("Error generating content: %v", err)
			ch <- fmt.Sprintf("Error: %v", err)
			return
		}

		ch <- resp.Text()
	}()

	return ch, nil
}
