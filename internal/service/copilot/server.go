package copilot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	assistant "github.com/qtopie/domour/gen/assistant/copilot" // Import the generated code
	cfg "github.com/qtopie/domour/internal/app/config"
	"github.com/qtopie/domour/internal/pkg/plugin"
	"github.com/qtopie/domour/internal/session"
	copilotPkg "github.com/qtopie/domour/pkg/copilot"
	"github.com/qtopie/domour/pkg/copilot/shared"
)

// ServiceServerImpl is the implementation of the CopilotService
type ServiceServerImpl struct {
	assistant.UnimplementedCopilotServiceServer
	pluginManager *plugin.PluginManager
	currentPlugin copilotPkg.CopilotPlugin
	currentName   string
	mu            sync.Mutex
	sessionStore  session.Store
}

// NewServiceServerImpl creates a new instance of ServiceServerImpl
func NewServiceServerImpl(pluginManager *plugin.PluginManager, store session.Store) *ServiceServerImpl {
	return &ServiceServerImpl{
		pluginManager: pluginManager,
		sessionStore:  store,
	}
}

// Copilot implements the Copilot RPC for streaming responses.
func (s *ServiceServerImpl) Copilot(req *assistant.CopilotRequest, stream assistant.CopilotService_CopilotServer) error {
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

	pluginStream, err := s.currentPlugin.Chat(shared.UserRequest{
		SessionId: req.SessionId,
		Seq:       req.Seq,
		Message:   req.Message,
		FrontPart: req.CodeBefore,
		BackPart:  req.CodeAfter,
		Filename:  req.Filename,
		Workspace: req.Workspace,
		History:   hist,
	})
	if err != nil {
		log.Printf("Error calling Chat on plugin %s: %v", s.currentName, err)
		return err
	}

	var replyBuilder strings.Builder
	for chunk := range pluginStream {
		resp := &assistant.CopilotResponse{
			SessionId: req.SessionId,
			Seq:       req.Seq,
			Patch:     chunk.Content,
			Complete:  chunk.IsLast,
		}
		if err := stream.Send(resp); err != nil {
			log.Printf("Error sending response to gRPC stream: %v", err)
			return err
		}
		replyBuilder.WriteString(chunk.Content)

		if chunk.IsLast {
			log.Printf("Received end signal from plugin %s", s.currentName)
			break
		}
	}

	if s.sessionStore != nil {
		reply := replyBuilder.String()
		_ = s.sessionStore.AppendHistory(context.Background(), req.SessionId, shared.Message{Role: "assistant", Content: reply, Time: time.Now().Unix()})
	}

	log.Printf("Copilot request completed for message: %s", req.Message)
	return nil
}

func (s *ServiceServerImpl) loadAndRefreshPlugin() error {
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
		copilotPlugin, ok := plugin.(copilotPkg.CopilotPlugin)
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
