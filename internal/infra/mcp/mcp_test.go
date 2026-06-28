package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("BE_MOCK_MCP_SERVER") == "1" {
		runMockMCPServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runMockMCPServer() {
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		var resp JSONRPCResponse
		resp.JSONRPC = "2.0"
		resp.ID = req.ID

		switch req.Method {
		case "initialize":
			resp.Result = InitializeResult{
				ProtocolVersion: "2024-11-05",
				ServerInfo: ImplementationInfo{
					Name:    "mock-server",
					Version: "1.0.0",
				},
			}
			data, _ := json.Marshal(resp)
			fmt.Printf("%s\n", data)

		case "notifications/initialized":
			// No response needed for notifications

		case "tools/list":
			resp.Result = ListToolsResult{
				Tools: []ToolDefinition{
					{
						Name:        "get_weather",
						Description: "Get the current weather",
						InputSchema: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"location": map[string]interface{}{
									"type": "string",
								},
							},
							"required": []interface{}{"location"},
						},
					},
				},
			}
			data, _ := json.Marshal(resp)
			fmt.Printf("%s\n", data)

		case "tools/call":
			resp.Result = CallToolResult{
				Content: []ContentBlock{
					{
						Type: "text",
						Text: "The weather in New York is sunny.",
					},
				},
			}
			data, _ := json.Marshal(resp)
			fmt.Printf("%s\n", data)
		}
	}
}

func TestStdioClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := NewStdioClient(os.Args[0], []string{"-test.run=TestStdioClient"}, map[string]string{
		"BE_MOCK_MCP_SERVER": "1",
	})
	defer client.Close()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	if len(tools) != 1 || tools[0].Name != "get_weather" {
		t.Errorf("Unexpected tools: %+v", tools)
	}

	result, err := client.CallTool(ctx, "get_weather", map[string]interface{}{"location": "New York"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	if len(result.Content) != 1 || result.Content[0].Text != "The weather in New York is sunny." {
		t.Errorf("Unexpected CallToolResult: %+v", result)
	}
}

func TestSSEClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sseDataChan := make(chan string, 5)
	
	tsMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sse" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)

			// 1. Send endpoint
			fmt.Fprintf(w, "event: endpoint\ndata: /message\n\n")
			flusher.Flush()

			// 2. Loop to forward test data
			for {
				select {
				case data, ok := <-sseDataChan:
					if !ok {
						return
					}
					fmt.Fprintf(w, "%s", data)
					flusher.Flush()
				case <-r.Context().Done():
					return
				}
			}
		}

		if r.URL.Path == "/message" {
			var req JSONRPCRequest
			_ = json.NewDecoder(r.Body).Decode(&req)

			// Broadcast response to client via SSE channel
			var resp JSONRPCResponse
			resp.JSONRPC = "2.0"
			resp.ID = req.ID
			if req.Method == "initialize" {
				resp.Result = InitializeResult{
					ProtocolVersion: "2024-11-05",
				}
			} else if req.Method == "tools/list" {
				resp.Result = ListToolsResult{
					Tools: []ToolDefinition{{Name: "test-sse-tool"}},
				}
			} else if req.Method == "tools/call" {
				resp.Result = CallToolResult{
					Content: []ContentBlock{{Type: "text", Text: "sse-ok"}},
				}
			}
			bytesData, _ := json.Marshal(resp)
			sseDataChan <- fmt.Sprintf("event: message\ndata: %s\n\n", string(bytesData))
			
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer tsMock.Close()
	defer close(sseDataChan)

	client := NewSSEClient(tsMock.URL + "/sse")
	defer client.Close()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("SSE Client Initialize failed: %v", err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("SSE Client ListTools failed: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "test-sse-tool" {
		t.Errorf("Unexpected SSE tools: %+v", tools)
	}

	res, err := client.CallTool(ctx, "test-sse-tool", nil)
	if err != nil {
		t.Fatalf("SSE Client CallTool failed: %v", err)
	}
	if len(res.Content) != 1 || res.Content[0].Text != "sse-ok" {
		t.Errorf("Unexpected SSE call tool result: %+v", res)
	}
}
