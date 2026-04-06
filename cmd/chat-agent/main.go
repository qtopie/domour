package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	assistant "github.com/qtopie/domour/gen/assistant/copilot"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// Address of the domour gRPC server
	addr := "localhost:1234"

	// Set up a connection to the server.
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	client := assistant.NewChatServiceClient(conn)

	// Create a bidirectional stream
	stream, err := client.Chat(context.Background())
	if err != nil {
		log.Fatalf("error opening stream: %v", err)
	}

	// Channel to signal completion
	done := make(chan bool)

	// Goroutine to receive events from the server
	go func() {
		for {
			event, err := stream.Recv()
			if err == io.EOF {
				fmt.Println("\n[Stream Closed]")
				close(done)
				return
			}
			if err != nil {
				log.Fatalf("error receiving event: %v", err)
			}

			switch e := event.Event.(type) {
			case *assistant.ChatEvent_Ack:
				fmt.Printf("[Ack] Message ID: %s, Created At: %d\n", event.MsgId, e.Ack.CreatedAt)
			case *assistant.ChatEvent_Delta:
				fmt.Print(e.Delta.ContentDelta)
			case *assistant.ChatEvent_Completed:
				fmt.Printf("\n[Completed] Final Content Length: %d, Reason: %v\n", len(e.Completed.FinalContent), e.Completed.Reason)
				close(done)
				return
			case *assistant.ChatEvent_Error:
				fmt.Printf("\n[Error] Code: %d, Message: %s\n", e.Error.Code, e.Error.Message)
				close(done)
				return
			}
		}
	}()

	// Send a test message
	req := &assistant.ChatRequest{
		ReqId: fmt.Sprintf("req-%d", time.Now().Unix()),
		Payload: &assistant.ChatRequest_PostMessage{
			PostMessage: &assistant.PostMessage{
				SessionId:      "test-session",
				ConversationId: "test-conv",
				SenderId:       "test-user",
				Content:        "Hello, can you help me test the new extensible Chat architecture?",
			},
		},
	}

	fmt.Printf("[Sending] %s\n", req.GetPostMessage().Content)
	if err := stream.Send(req); err != nil {
		log.Fatalf("error sending request: %v", err)
	}

	// Wait for the stream to finish
	<-done
}
