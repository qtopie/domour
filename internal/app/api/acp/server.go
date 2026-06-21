package acpapi

import (
	"context"
	"encoding/json"
	"io"
	"log"

	"github.com/cloudwego/eino/components/model"
	"github.com/qtopie/domour/ark/acp"
	"github.com/qtopie/domour/internal/brain"
	"github.com/qtopie/domour/internal/engine"
)

type Server struct {
	brainNode *brain.DiencephalonNode
	chatModel model.ChatModel
	engine    engine.Engine
}

func NewServer(brainNode *brain.DiencephalonNode, chatModel model.ChatModel, eng engine.Engine) *Server {
	return &Server{
		brainNode: brainNode,
		chatModel: chatModel,
		engine:    eng,
	}
}

func (s *Server) Start(ctx context.Context, transport Transport) error {
	sess := NewSession(transport)
	log.Printf("Starting ACP session %s", sess.ID())
	defer sess.Close()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			req, err := transport.ReadMessage(ctx)
			if err != nil {
				if err == io.EOF {
					return nil
				}
				log.Printf("Error reading message: %v", err)
				return err
			}

			resp, err := sess.HandleMessage(ctx, req)
			if err != nil {
				log.Printf("Error handling message: %v", err)
				continue
			}

			if resp != nil {
				if err := transport.WriteMessage(ctx, resp); err != nil {
					log.Printf("Error writing response: %v", err)
					return err
				}

				// If it was the initialize message, we now know the mode and can set the handler.
				if req.Method == acp.MethodInitialize {
					if sess.Mode() == acp.ModeProxy {
						sess.SetHandler(NewProxyHandler(s.chatModel))
					} else {
						sess.SetHandler(NewCognitiveHandler(s.brainNode, s.engine, func(ctx context.Context, method string, params any) error {
							paramsBytes, _ := json.Marshal(params)
							return transport.WriteMessage(ctx, &acp.JSONRPCRequest{
								JSONRPC: "2.0",
								Method:  method,
								Params:  paramsBytes,
							})
						}))
					}
				}
			}
		}
	}
}
