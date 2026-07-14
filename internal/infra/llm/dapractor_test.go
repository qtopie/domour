package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	dapr_v1 "github.com/dapr/dapr/pkg/proto/runtime/v1"
	"google.golang.org/grpc"
)

type mockDaprServer struct {
	dapr_v1.UnimplementedDaprServer
	invokeActorHandler func(ctx context.Context, in *dapr_v1.InvokeActorRequest) (*dapr_v1.InvokeActorResponse, error)
}

func (s *mockDaprServer) InvokeActor(ctx context.Context, in *dapr_v1.InvokeActorRequest) (*dapr_v1.InvokeActorResponse, error) {
	if s.invokeActorHandler != nil {
		return s.invokeActorHandler(ctx, in)
	}
	return nil, fmt.Errorf("InvokeActor not implemented in mock")
}

func TestDaprActorChatModel(t *testing.T) {
	// 1. Setup mock Dapr gRPC server
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer lis.Close()

	grpcServer := grpc.NewServer()
	mockSrv := &mockDaprServer{}
	dapr_v1.RegisterDaprServer(grpcServer, mockSrv)

	go func() {
		if err := grpcServer.Serve(lis); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			t.Errorf("grpc server exited: %v", err)
		}
	}()
	defer grpcServer.Stop()

	// Extract port and set environment variables for Dapr client
	_, portStr, err := net.SplitHostPort(lis.Addr().String())
	if err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}
	t.Setenv("DAPR_GRPC_PORT", portStr)

	// Mock InvokeActor to act as the ModelActor
	mockSrv.invokeActorHandler = func(ctx context.Context, in *dapr_v1.InvokeActorRequest) (*dapr_v1.InvokeActorResponse, error) {
		if in.ActorType != "ModelActor" {
			return nil, fmt.Errorf("unexpected actor type: %s", in.ActorType)
		}
		if in.ActorId != "gemma-test" {
			return nil, fmt.Errorf("unexpected actor id: %s", in.ActorId)
		}
		if in.Method != "ChatCompletions" {
			return nil, fmt.Errorf("unexpected method: %s", in.Method)
		}

		var req openaiRequest
		if err := json.Unmarshal(in.Data, &req); err != nil {
			return nil, fmt.Errorf("failed to unmarshal request data: %w", err)
		}

		if len(req.Messages) == 0 {
			return nil, fmt.Errorf("empty messages in request")
		}

		// Mock response
		resp := openaiResponse{
			Choices: []struct {
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
			}{
				{
					Message: struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					}{
						Role:    "assistant",
						Content: fmt.Sprintf("Echo: %s", req.Messages[len(req.Messages)-1].Content),
					},
				},
			},
		}

		respBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		return &dapr_v1.InvokeActorResponse{
			Data: respBytes,
		}, nil
	}

	// 2. Test Model Factory
	cfg := &Config{
		Provider: "dapr-actor",
		Model:    "gemma-test",
	}

	model, err := NewChatModel(context.Background(), cfg)
	if err != nil {
		t.Fatalf("failed to create dapr-actor model: %v", err)
	}

	// 3. Test Generate
	messages := []*schema.Message{
		schema.UserMessage("hello actor"),
	}

	resp, err := model.Generate(context.Background(), messages)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if resp.Content != "Echo: hello actor" {
		t.Errorf("unexpected response content: %s", resp.Content)
	}

	// 4. Test Stream
	sr, err := model.Stream(context.Background(), messages)
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	defer sr.Close()

	chunk, err := sr.Recv()
	if err != nil {
		t.Fatalf("Stream Recv failed: %v", err)
	}

	if chunk.Content != "Echo: hello actor" {
		t.Errorf("unexpected stream chunk content: %s", chunk.Content)
	}
}
