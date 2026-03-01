package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/qtopie/homa/gen/assistant" // Import the generated code
	cfg "github.com/qtopie/homa/internal/app/config"
	"github.com/qtopie/homa/internal/assistant/plugins/copilot/shared"
	"github.com/qtopie/homa/internal/session"
)

// CopilotServiceServerImpl is the implementation of the ChatService
type CopilotServiceServerImpl struct {
	assistant.UnimplementedCopilotServiceServer
	pluginManager *PluginManager
	currentPlugin CopilotPlugin
	currentName   string
	mu            sync.Mutex
	sessionStore  session.Store
}

// NewCopilotServiceServerImpl creates a new instance of CopilotServiceServerImpl
func NewCopilotServiceServerImpl(pluginManager *PluginManager) *CopilotServiceServerImpl {
	var store session.Store
	var err error

	// Default to buntdb
	dbPath := cfg.GetAppConfig().GetString("buntdb.path")
	if dbPath == "" {
		dbPath = "sessions.db"
	}
	store, err = session.NewBuntDBStore(dbPath, 10, 0)
	if err != nil {
		log.Printf("failed to create buntdb session store: %v", err)
		store = nil
	}

	return &CopilotServiceServerImpl{
		pluginManager: pluginManager,
		sessionStore:  store,
	}
}

// Chat implements the server streaming method for ChatService
func (s *CopilotServiceServerImpl) Chat(req *assistant.UserRequest, stream assistant.CopilotService_ChatServer) error {
	err := s.loadAndRefreshPlugin()
	if err != nil {
		log.Println("failed to load plugin", err)
		return err
	}

	// Load session history and persist user message
	var hist []shared.Message
	if s.sessionStore != nil {
		if h, err := s.sessionStore.GetHistory(context.Background(), req.SessionId); err == nil {
			hist = h
		}
		_ = s.sessionStore.AppendHistory(context.Background(), req.SessionId, shared.Message{Role: "user", Content: req.Message, Time: time.Now().Unix()})
	}

	// Forward the request to the plugin's Chat method
	pluginStream, err := s.currentPlugin.Chat(shared.UserRequest{
		SessionId: req.SessionId,
		Seq:       req.Seq,
		Message:   req.Message,
		FrontPart: req.FrontPart,
		BackPart:  req.BackPart,
		Filename:  req.Filename,
		Workspace: req.Workspace,
		History:   hist,
	})
	if err != nil {
		log.Printf("Error calling Chat on plugin %s: %v", s.currentName, err)
		return err
	}
	// Consume the plugin's stream and forward to gRPC stream
	var replyBuilder strings.Builder
	for chunk := range pluginStream {
		// Send each chunk to the gRPC stream
		resp := &assistant.StreamResponse{
			Content: chunk.Content,
		}
		if err := stream.Send(resp); err != nil {
			log.Printf("Error sending response to gRPC stream: %v", err)
			return err
		}
		replyBuilder.WriteString(chunk.Content)

		// Check if this is the last chunk
		if chunk.IsLast {
			log.Printf("Received end signal from plugin %s", s.currentName)
			break
		}
	}

	// Persist assistant reply to session history
	if s.sessionStore != nil {
		reply := replyBuilder.String()
		_ = s.sessionStore.AppendHistory(context.Background(), req.SessionId, shared.Message{Role: "assistant", Content: reply, Time: time.Now().Unix()})
	}

	log.Printf("Chat request completed for message: %s", req.Message)
	return nil
}

// AutoComplete implements the unary method for AutoComplete
func (s *CopilotServiceServerImpl) AutoComplete(ctx context.Context, req *assistant.UserRequest) (*assistant.AgentResponse, error) {
	err := s.loadAndRefreshPlugin()
	if err != nil {
		log.Println("failed to load plugin", err)
		return nil, err
	}

	// Load session history and persist user message
	var hist []shared.Message
	if s.sessionStore != nil {
		if h, err := s.sessionStore.GetHistory(context.Background(), req.SessionId); err == nil {
			hist = h
		}
		_ = s.sessionStore.AppendHistory(context.Background(), req.SessionId, shared.Message{Role: "user", Content: req.Message, Time: time.Now().Unix()})
	}

	// Forward the request to the plugin's AutoComplete method
	reply, err := s.currentPlugin.AutoComplete(shared.UserRequest{
		SessionId: req.SessionId,
		Seq:       req.Seq,
		Message:   req.Message,
		FrontPart: req.FrontPart,
		BackPart:  req.BackPart,
		Filename:  req.Filename,
		Workspace: req.Workspace,
		History:   hist,
	})
	if err != nil {
		log.Printf("Error calling Chat on plugin %s: %v", s.currentName, err)
		return nil, err
	}
	log.Println("autocomplete", req.Message, "response", reply)

	resp := &assistant.AgentResponse{
		Content: reply,
	}

	// Persist assistant reply
	if s.sessionStore != nil {
		_ = s.sessionStore.AppendHistory(ctx, req.SessionId, shared.Message{Role: "assistant", Content: reply, Time: time.Now().Unix()})
	}
	return resp, nil
}

func (s *CopilotServiceServerImpl) loadAndRefreshPlugin() error {
	// Get the plugin name from the configuration
	copilotPluginName := cfg.GetAppConfig().GetString("plugins.copilot")
	if copilotPluginName == "" {
		return fmt.Errorf("no copilot plugin specified in configuration")
	}

	// Load the plugin only if it has changed
	s.mu.Lock()
	if s.currentName != copilotPluginName {
		log.Printf("Loading copilot plugin: %s", copilotPluginName)

		// Load the plugin dynamically
		err := s.pluginManager.LoadPlugin("copilot", copilotPluginName)
		if err != nil {
			s.mu.Unlock()
			log.Printf("Error loading copilot plugin %s: %v", copilotPluginName, err)
			return err
		}

		// Retrieve the loaded plugin
		plugin, exists := s.pluginManager.GetPlugin("copilot", copilotPluginName)
		if !exists {
			s.mu.Unlock()
			return fmt.Errorf("copilot plugin %s not found", copilotPluginName)
		}

		// Assert the plugin to the CopilotPlugin interface
		copilotPlugin, ok := plugin.(CopilotPlugin)
		if !ok {
			s.mu.Unlock()
			return fmt.Errorf("plugin %s does not implement CopilotPlugin interface", copilotPluginName)
		}

		// Update the current plugin and name
		s.currentPlugin = copilotPlugin
		s.currentName = copilotPluginName
	}
	s.mu.Unlock()
	return nil
}
