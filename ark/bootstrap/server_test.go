package bootstrap

import (
	"context"
	"testing"

	"google.golang.org/grpc"
)

func TestRunWithGRPCServer(t *testing.T) {
	s := grpc.NewServer()
	
	// We use a background context and expect it to return immediately
	// because it just registers and doesn't block.
	err := Run(context.Background(), WithGRPCServer(s))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify that services are registered. 
	// This is a bit hard to do via public API without reflection, 
	// but we can check if it returns without error and doesn't block.
}
