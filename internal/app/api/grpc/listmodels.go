package grpc

import (
	"context"

	chatpb "github.com/qtopie/domour/gen/assistant/chat"
)

// ListModels returns all available models from the global registry and
// Domour's configured providers.
func (s *Server) ListModels(ctx context.Context, req *chatpb.ListModelsRequest) (*chatpb.ListModelsResponse, error) {
	models, err := s.app.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	return &chatpb.ListModelsResponse{Models: models}, nil
}
