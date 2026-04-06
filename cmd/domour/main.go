package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/qtopie/domour/internal/pkg/bootstrap"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := bootstrap.Run(ctx); err != nil {
		log.Fatalf("domour server exited: %v", err)
	}
}
