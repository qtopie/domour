package chat

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	assistant "github.com/qtopie/domour/gen/assistant/copilot"
	cfg "github.com/qtopie/domour/internal/app/config"
	"github.com/qtopie/domour/internal/pkg/copilot/shared"
	"github.com/qtopie/domour/internal/pkg/plugin"
	"github.com/qtopie/domour/internal/service/chat/provider"
	"github.com/qtopie/domour/internal/session"
	"google.golang.org/grpc"
)

type ServiceServerImpl struct {
	assistant.UnimplementedChatServiceServer

	providers    *provider.Registry
	sessionStore session.Store

	mu      sync.Mutex
	cancel  map[string]context.CancelFunc
	counter uint64
}

func NewServiceServerImpl(pluginManager *plugin.PluginManager, store session.Store) *ServiceServerImpl {
	registry := provider.NewRegistry()
	registry.Register(provider.NewMockProvider())
	registry.Register(provider.NewGeminiProvider())
	registry.Register(provider.NewCLIProvider("echo", "Replying to:"))
	registry.Register(provider.NewWebSocketProvider("ws://localhost:8080/chat"))
	if pluginManager != nil {
		registry.Register(provider.NewCopilotPluginProvider(pluginManager))
	}

	return &ServiceServerImpl{
		providers:    registry,
		sessionStore: store,
		cancel:       make(map[string]context.CancelFunc),
	}
}

func (s *ServiceServerImpl) Chat(stream grpc.BidiStreamingServer[assistant.ChatRequest, assistant.ChatEvent]) error {
	var wg sync.WaitGroup
	sendMu := &sync.Mutex{}

	send := func(event *assistant.ChatEvent) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		return stream.Send(event)
	}

	for {
		req, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				wg.Wait()
				return nil
			}
			wg.Wait()
			return err
		}

		if post := req.GetPostMessage(); post != nil {
			wg.Add(1)
			go func(reqID string, post *assistant.PostMessage) {
				defer wg.Done()
				s.processPost(stream.Context(), send, reqID, post)
			}(req.GetReqId(), post)
			continue
		}

		if control := req.GetControl(); control != nil {
			s.processControl(send, req.GetReqId(), control)
		}
	}
}

func (s *ServiceServerImpl) processPost(parent context.Context, send func(*assistant.ChatEvent) error, reqID string, post *assistant.PostMessage) {
	msgID := s.newMsgID()
	ctx, cancel := context.WithCancel(parent)
	s.storeCancel(msgID, cancel)
	defer s.clearCancel(msgID)
	defer cancel()

	_ = send(&assistant.ChatEvent{
		ReqId: reqID,
		MsgId: msgID,
		Event: &assistant.ChatEvent_Ack{Ack: &assistant.MessageAck{CreatedAt: time.Now().Unix()}},
	})

	providerName := cfg.GetAppConfig().GetString("chat.provider")
	if providerName == "" {
		providerName = "copilot-plugin"
	}
	if post.GetContext() != nil {
		if value, ok := post.GetContext().AsMap()["provider"]; ok {
			if cast, ok := value.(string); ok && cast != "" {
				providerName = cast
			}
		}
	}

	history := s.getHistory(post.GetSessionId())
	chatProvider, err := s.providers.Get(providerName)
	if err != nil {
		s.sendError(send, reqID, msgID, 404, err.Error())
		_ = send(&assistant.ChatEvent{
			ReqId: reqID,
			MsgId: msgID,
			Event: &assistant.ChatEvent_Completed{Completed: &assistant.MessageCompleted{Reason: assistant.MessageCompleted_ERROR}},
		})
		return
	}

	ch, err := chatProvider.Generate(ctx, provider.GenerateRequest{
		SessionID:      post.GetSessionId(),
		ConversationID: post.GetConversationId(),
		SenderID:       post.GetSenderId(),
		Content:        post.GetContent(),
		History:        history,
		Context: func() map[string]any {
			if post.GetContext() == nil {
				return nil
			}
			return post.GetContext().AsMap()
		}(),
	})
	if err != nil {
		s.sendError(send, reqID, msgID, 500, err.Error())
		_ = send(&assistant.ChatEvent{
			ReqId: reqID,
			MsgId: msgID,
			Event: &assistant.ChatEvent_Completed{Completed: &assistant.MessageCompleted{Reason: assistant.MessageCompleted_ERROR}},
		})
		return
	}

	var final string
	for {
		select {
		case <-ctx.Done():
			_ = send(&assistant.ChatEvent{
				ReqId: reqID,
				MsgId: msgID,
				Event: &assistant.ChatEvent_Completed{Completed: &assistant.MessageCompleted{FinalContent: final, Reason: assistant.MessageCompleted_CANCELLED}},
			})
			return
		case chunk, ok := <-ch:
			if !ok {
				s.appendHistory(post, final)
				_ = send(&assistant.ChatEvent{
					ReqId: reqID,
					MsgId: msgID,
					Event: &assistant.ChatEvent_Completed{Completed: &assistant.MessageCompleted{FinalContent: final, Reason: assistant.MessageCompleted_STOP}},
				})
				return
			}

			if chunk.Err != nil {
				s.sendError(send, reqID, msgID, 500, chunk.Err.Error())
				_ = send(&assistant.ChatEvent{
					ReqId: reqID,
					MsgId: msgID,
					Event: &assistant.ChatEvent_Completed{Completed: &assistant.MessageCompleted{FinalContent: final, Reason: assistant.MessageCompleted_ERROR}},
				})
				return
			}

			if chunk.Text != "" {
				final += chunk.Text
				_ = send(&assistant.ChatEvent{
					ReqId: reqID,
					MsgId: msgID,
					Event: &assistant.ChatEvent_Delta{Delta: &assistant.MessageDelta{ContentDelta: chunk.Text}},
				})
			}

			if chunk.Done {
				s.appendHistory(post, final)
				_ = send(&assistant.ChatEvent{
					ReqId: reqID,
					MsgId: msgID,
					Event: &assistant.ChatEvent_Completed{Completed: &assistant.MessageCompleted{FinalContent: final, Reason: assistant.MessageCompleted_STOP}},
				})
				return
			}
		}
	}
}

func (s *ServiceServerImpl) processControl(send func(*assistant.ChatEvent) error, reqID string, control *assistant.Control) {
	if control.GetCommand() != assistant.Control_CANCEL {
		return
	}

	target := control.GetTargetMsgId()
	s.mu.Lock()
	cancel, ok := s.cancel[target]
	s.mu.Unlock()
	if !ok {
		s.sendError(send, reqID, target, 404, fmt.Sprintf("target message %s not running", target))
		return
	}
	cancel()
}

func (s *ServiceServerImpl) sendError(send func(*assistant.ChatEvent) error, reqID, msgID string, code int32, message string) {
	_ = send(&assistant.ChatEvent{
		ReqId: reqID,
		MsgId: msgID,
		Event: &assistant.ChatEvent_Error{Error: &assistant.Error{Code: code, Message: message}},
	})
}

func (s *ServiceServerImpl) getHistory(sessionID string) []provider.Message {
	if s.sessionStore == nil || sessionID == "" {
		return nil
	}
	items, err := s.sessionStore.GetHistory(context.Background(), sessionID)
	if err != nil {
		return nil
	}
	result := make([]provider.Message, 0, len(items))
	for _, item := range items {
		result = append(result, provider.Message{Role: item.Role, Content: item.Content})
	}
	return result
}

func (s *ServiceServerImpl) appendHistory(post *assistant.PostMessage, reply string) {
	if s.sessionStore == nil || post.GetSessionId() == "" {
		return
	}
	_ = s.sessionStore.AppendHistory(context.Background(), post.GetSessionId(), shared.Message{Role: "user", Content: post.GetContent(), Time: time.Now().Unix()})
	_ = s.sessionStore.AppendHistory(context.Background(), post.GetSessionId(), shared.Message{Role: "assistant", Content: reply, Time: time.Now().Unix()})
}

func (s *ServiceServerImpl) storeCancel(msgID string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel[msgID] = cancel
}

func (s *ServiceServerImpl) clearCancel(msgID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cancel, msgID)
}

func (s *ServiceServerImpl) newMsgID() string {
	next := atomic.AddUint64(&s.counter, 1)
	return fmt.Sprintf("msg-%d-%d", time.Now().UnixNano(), next)
}
