package server

import (
	"context"

	"github.com/qtopie/domour/internal/bootstrap"
)

// Run starts the Domour server using the module's default bootstrap sequence.
func Run(ctx context.Context) error {
	return bootstrap.Run(ctx)
}
