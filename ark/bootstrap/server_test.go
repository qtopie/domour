package bootstrap

import (
	"context"
	"testing"

	"google.golang.org/grpc"
)

func TestRunWithGRPCServer(t *testing.T) {
	s := grpc.NewServer()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so background loop terminates gracefully

	err := Run(ctx, WithGRPCServer(s))
	if err != nil && err != context.Canceled {
		t.Fatalf("Run failed: %v", err)
	}
}
