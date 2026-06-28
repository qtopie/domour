package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	daprClient "github.com/dapr/go-sdk/client"
)

type DaprClient struct {
	appID string

	client daprClient.Client
}

func NewDaprClient(appID string) *DaprClient {
	return &DaprClient{
		appID: appID,
	}
}

func (c *DaprClient) Initialize(ctx context.Context) error {
	if c.client != nil {
		return nil
	}

	port := os.Getenv("DAPR_GRPC_PORT")
	if port == "" {
		port = "50001"
	}

	dc, err := daprClient.NewClientWithPort(port)
	if err != nil {
		return fmt.Errorf("create dapr client: %w", err)
	}

	c.client = dc
	return nil
}

func (c *DaprClient) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	if c.client == nil {
		return nil, fmt.Errorf("dapr client not initialized")
	}

	// Invoke remote app ID method "list-tools"
	resp, err := c.client.InvokeMethod(ctx, c.appID, "mcp/list-tools", "GET")
	if err != nil {
		return nil, fmt.Errorf("dapr invoke list-tools: %w", err)
	}

	var tools []ToolDefinition
	if err := json.Unmarshal(resp, &tools); err != nil {
		return nil, fmt.Errorf("unmarshal tools: %w", err)
	}

	return tools, nil
}

func (c *DaprClient) CallTool(ctx context.Context, name string, arguments map[string]interface{}) (*CallToolResult, error) {
	if c.client == nil {
		return nil, fmt.Errorf("dapr client not initialized")
	}

	params := CallToolParams{
		Name:      name,
		Arguments: arguments,
	}

	data, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	// Invoke remote app ID method "call-tool"
	resp, err := c.client.InvokeMethodWithContent(ctx, c.appID, "mcp/call-tool", "POST", &daprClient.DataContent{
		ContentType: "application/json",
		Data:        data,
	})
	if err != nil {
		return nil, fmt.Errorf("dapr invoke call-tool: %w", err)
	}

	var result CallToolResult
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("unmarshal call tool result: %w", err)
	}

	return &result, nil
}

func (c *DaprClient) Close() error {
	if c.client != nil {
		c.client.Close()
		c.client = nil
	}
	return nil
}
