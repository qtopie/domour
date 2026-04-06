package copilot

import "github.com/qtopie/domour/internal/pkg/copilot/shared"

type CopilotPlugin interface {
	Chat(shared.UserRequest) (<-chan shared.ChunkData, error)

	AutoComplete(shared.UserRequest) (string, error)
}
