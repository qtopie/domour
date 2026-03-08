package main

import (
	"fmt"
	"time"

	"github.com/qtopie/domour/pkg/copilot/shared"
)

// MockCopilotPlugin is a mock implementation of the CopilotPlugin interface
type MockCopilotPlugin struct{}

// Chat simulates streaming data chunks to the client
func (p MockCopilotPlugin) Chat(req shared.UserRequest) (<-chan shared.ChunkData, error) {
	ch := make(chan shared.ChunkData)

	go func() {
		defer close(ch) // Ensure the channel is closed when done

		// Simulate sending 5 chunks of data
		for i := 1; i <= 5; i++ {
			ch <- shared.ChunkData{
				ID:      fmt.Sprintf("%d", i),
				Content: fmt.Sprintf("Chunk %d: %s", i, req.Message),
				IsLast:  i == 5, // Mark the last chunk
			}
			time.Sleep(500 * time.Millisecond) // Simulate delay
		}
	}()

	return ch, nil
}

// AutoComplete simulates generating a single response
func (p MockCopilotPlugin) AutoComplete(req shared.UserRequest) (string, error) {
	return fmt.Sprintf("AutoComplete response for: %s", req.Message), nil
}

// Export the mock plugin instance
var Plugin MockCopilotPlugin
