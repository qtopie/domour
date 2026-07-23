package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/config"
	"github.com/qtopie/domour/internal/infra/mcp"
)

// mcpClientAdapter wraps mcp.Client to implement MCPToolClient
type mcpClientAdapter struct {
	client mcp.Client
}

func (a *mcpClientAdapter) CallTool(ctx context.Context, name string, args map[string]interface{}) (MCPCallResult, error) {
	res, err := a.client.CallTool(ctx, name, args)
	if err != nil {
		return MCPCallResult{}, err
	}
	var contentBuilder strings.Builder
	for _, block := range res.Content {
		if block.Type == "text" {
			contentBuilder.WriteString(block.Text)
		}
	}
	return MCPCallResult{
		Content: contentBuilder.String(),
	}, nil
}

func (a *mcpClientAdapter) Close(ctx context.Context) error {
	_ = ctx
	return a.client.Close()
}

// ConvertJSONSchemaToEinoParams converts an MCP input schema map (JSON Schema) to Eino's native schema.ParamsOneOf
func ConvertJSONSchemaToEinoParams(inputSchema map[string]interface{}) *schema.ParamsOneOf {
	if inputSchema == nil {
		return nil
	}

	properties, _ := inputSchema["properties"].(map[string]interface{})
	if len(properties) == 0 {
		return nil
	}

	requiredList, _ := inputSchema["required"].([]interface{})
	requiredSet := make(map[string]bool)
	for _, req := range requiredList {
		if s, ok := req.(string); ok {
			requiredSet[s] = true
		}
	}

	params := make(map[string]*schema.ParameterInfo)
	for name, prop := range properties {
		propMap, ok := prop.(map[string]interface{})
		if !ok {
			continue
		}

		pTypeStr, _ := propMap["type"].(string)
		var pType schema.DataType
		switch pTypeStr {
		case "string":
			pType = schema.String
		case "integer", "number":
			pType = schema.Integer
		case "boolean":
			pType = schema.Boolean
		case "array":
			pType = schema.Array
		case "object":
			pType = schema.Object
		default:
			pType = schema.String
		}

		desc, _ := propMap["description"].(string)

		var enumVals []string
		if enumList, ok := propMap["enum"].([]interface{}); ok {
			for _, ev := range enumList {
				if s, ok := ev.(string); ok {
					enumVals = append(enumVals, s)
				}
			}
		}

		params[name] = &schema.ParameterInfo{
			Type:     pType,
			Desc:     desc,
			Required: requiredSet[name],
			Enum:     enumVals,
		}
	}

	return schema.NewParamsOneOfByParams(params)
}

// LoadMCPServers reads config and registers all MCP tools in the Manager
func (m *Manager) LoadMCPServers(ctx context.Context) error {
	return m.loadMCPServers(ctx)
}

// ReloadMCPServers unregisters all existing MCP tools and re-loads them from config.
func (m *Manager) ReloadMCPServers(ctx context.Context) error {
	m.mu.Lock()
	// Unregister all MCP tools
	for name, state := range m.tools {
		if state.spec.Kind == ToolKindMCP {
			if state.runtime != nil {
				_ = state.runtime.Close(ctx)
			}
			delete(m.tools, name)
		}
	}
	m.mu.Unlock()
	return m.loadMCPServers(ctx)
}

// loadMCPServers is the internal implementation shared by LoadMCPServers and ReloadMCPServers.
func (m *Manager) loadMCPServers(ctx context.Context) error {
	cfg, err := config.LoadDomourConfig()
	if err != nil {
		return fmt.Errorf("load config for mcp servers: %w", err)
	}

	if cfg.MCPServers == nil {
		cfg.MCPServers = make(map[string]config.MCPServerConfig)
	}

	// 1. Load user-level MCP configs: ~/.domour/mcp_config.json (and fallback to mcp.json)
	userMcpPaths := []string{
		filepath.Join(config.DomourHomeDir(), "mcp_config.json"),
		filepath.Join(config.DomourHomeDir(), "mcp.json"),
	}
	for _, mcpPath := range userMcpPaths {
		if data, err := os.ReadFile(mcpPath); err == nil {
			loadMCPServerMap(data, cfg.MCPServers)
		}
	}

	// 2. Load project-level MCP configs: .agents/mcp_config.json in current working directory
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		projMcpPath := filepath.Join(cwd, ".agents", "mcp_config.json")
		if data, err := os.ReadFile(projMcpPath); err == nil {
			loadMCPServerMap(data, cfg.MCPServers)
		}
	}

	// 3. Load modular MCP configs from the mcp_dir if it exists
	mcpDir := strings.TrimSpace(cfg.MCPDir)
	if mcpDir != "" {
		if info, err := os.Stat(mcpDir); err == nil && info.IsDir() {
			entries, err := os.ReadDir(mcpDir)
			if err == nil {
				for _, entry := range entries {
					if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
						continue
					}
					path := filepath.Join(mcpDir, entry.Name())
					data, err := os.ReadFile(path)
					if err != nil {
						slog.Warn("Failed to read modular MCP config file", "path", path, "error", err)
						continue
					}
					var serverCfg config.MCPServerConfig
					if err := json.Unmarshal(data, &serverCfg); err != nil {
						slog.Warn("Failed to parse modular MCP config file", "path", path, "error", err)
						continue
					}
					serverName := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
					// Only register if not overridden in inline config / mcp_config.json
					if _, exists := cfg.MCPServers[serverName]; !exists {
						cfg.MCPServers[serverName] = serverCfg
					}
				}
			}
		}
	}

	// 2. Initialize each server
	for serverName, serverCfg := range cfg.MCPServers {
		var client mcp.Client
		switch serverCfg.Type {
		case "stdio":
			client = mcp.NewStdioClient(serverCfg.Command, serverCfg.Args, serverCfg.Env)
		case "sse":
			client = mcp.NewSSEClient(serverCfg.URL)
		case "dapr":
			client = mcp.NewDaprClient(serverCfg.AppID)
		default:
			slog.Warn("Unsupported MCP server type", "server", serverName, "type", serverCfg.Type)
			continue
		}

		slog.Info("Initializing MCP server connection", "server", serverName, "type", serverCfg.Type)
		if err := client.Initialize(ctx); err != nil {
			slog.Error("Failed to initialize MCP server", "server", serverName, "error", err)
			continue
		}

		tools, err := client.ListTools(ctx)
		if err != nil {
			client.Close()
			slog.Error("Failed to list tools from MCP server", "server", serverName, "error", err)
			continue
		}

		slog.Info("Successfully fetched tools from MCP server", "server", serverName, "count", len(tools))
		for _, t := range tools {
			qualifiedName := fmt.Sprintf("%s.%s", serverName, t.Name)
			params := ConvertJSONSchemaToEinoParams(t.InputSchema)

			err := m.Register(NewMCPToolWithParams(qualifiedName, t.Name, t.Description, params, func(ctx context.Context) (MCPToolClient, error) {
				return &mcpClientAdapter{client: client}, nil
			}))
			if err != nil {
				client.Close()
				slog.Error("Failed to register MCP tool", "tool", qualifiedName, "error", err)
				break
			}
			slog.Debug("Registered MCP tool", "name", qualifiedName, "description", t.Description)
		}
	}

	return nil
}

func loadMCPServerMap(data []byte, target map[string]config.MCPServerConfig) {
	var mcpWrap struct {
		MCPServers map[string]config.MCPServerConfig `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &mcpWrap); err == nil && len(mcpWrap.MCPServers) > 0 {
		for name, sCfg := range mcpWrap.MCPServers {
			if sCfg.Type == "" {
				sCfg.Type = "stdio"
			}
			target[name] = sCfg
		}
		return
	}

	var directMap map[string]config.MCPServerConfig
	if err := json.Unmarshal(data, &directMap); err == nil {
		for name, sCfg := range directMap {
			if sCfg.Type == "" {
				sCfg.Type = "stdio"
			}
			target[name] = sCfg
		}
	}
}
