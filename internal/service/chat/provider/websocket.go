package provider

import (
	"context"
	"log"

	"github.com/gorilla/websocket"
)

type WebSocketProvider struct {
	url string
}

func NewWebSocketProvider(url string) *WebSocketProvider {
	return &WebSocketProvider{url: url}
}

func (p *WebSocketProvider) Name() string {
	return "websocket"
}

func (p *WebSocketProvider) Generate(ctx context.Context, req GenerateRequest) (<-chan Chunk, error) {
	conn, _, err := websocket.DefaultDialer.Dial(p.url, nil)
	if err != nil {
		return nil, err
	}
	
	out := make(chan Chunk)
	
	// Send request over websocket
	if err := conn.WriteJSON(req); err != nil {
		conn.Close()
		return nil, err
	}
	
	go func() {
		defer close(out)
		defer conn.Close()
		
		for {
			select {
			case <-ctx.Done():
				return
			default:
				_, message, err := conn.ReadMessage()
				if err != nil {
					log.Printf("WS read error: %v", err)
					out <- Chunk{Err: err}
					return
				}
				
				// Assuming the message is the chunk content
				out <- Chunk{Text: string(message)}
				
				// Need a protocol to know when it's done. 
				// For now, let's assume one message for simplicity or check some EOF marker.
			}
		}
	}()
	
	return out, nil
}
