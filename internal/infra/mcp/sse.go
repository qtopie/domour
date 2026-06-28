package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type SSEClient struct {
	sseURL string

	httpClient *http.Client
	postURL    string
	postURLMu  sync.RWMutex

	nextID      int64
	pendingMu   sync.Mutex
	pending     map[string]chan *JSONRPCResponse
	initialized bool

	closeOnce sync.Once
	done      chan struct{}
	sseBody   io.ReadCloser
}

func NewSSEClient(sseURL string) *SSEClient {
	return &SSEClient{
		sseURL:     sseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		pending:    make(map[string]chan *JSONRPCResponse),
		done:       make(chan struct{}),
	}
}

func (c *SSEClient) Initialize(ctx context.Context) error {
	c.pendingMu.Lock()
	if c.initialized {
		c.pendingMu.Unlock()
		return nil
	}
	c.pendingMu.Unlock()

	// 1. Connect to SSE stream
	req, err := http.NewRequestWithContext(ctx, "GET", c.sseURL, nil)
	if err != nil {
		return fmt.Errorf("create sse request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to sse: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("sse connection returned status: %d", resp.StatusCode)
	}

	c.sseBody = resp.Body

	endpointChan := make(chan string, 1)

	// Start reading the SSE stream in a background loop
	go c.readSSELoop(resp.Body, endpointChan)

	// Wait for the POST URL/endpoint event from SSE with a timeout
	var relativePostURL string
	select {
	case <-ctx.Done():
		_ = c.Close()
		return ctx.Err()
	case <-time.After(10 * time.Second):
		_ = c.Close()
		return fmt.Errorf("timeout waiting for sse endpoint event")
	case relativePostURL = <-endpointChan:
	}

	// Resolve relative POST URL against SSE base URL
	base, err := url.Parse(c.sseURL)
	if err != nil {
		_ = c.Close()
		return fmt.Errorf("parse sse url: %w", err)
	}
	ref, err := url.Parse(relativePostURL)
	if err != nil {
		_ = c.Close()
		return fmt.Errorf("parse post url reference %s: %w", relativePostURL, err)
	}
	resolvedPostURL := base.ResolveReference(ref).String()

	c.postURLMu.Lock()
	c.postURL = resolvedPostURL
	c.postURLMu.Unlock()

	// 2. Perform Handshake via HTTP POST
	id := atomic.AddInt64(&c.nextID, 1)
	initReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "initialize",
		Params: InitializeParams{
			ProtocolVersion: "2024-11-05",
			Capabilities:    ClientCapabilities{},
			ClientInfo: ImplementationInfo{
				Name:    "domour",
				Version: "0.1.0",
			},
		},
	}

	initResp, err := c.sendRequest(ctx, id, initReq)
	if err != nil {
		_ = c.Close()
		return fmt.Errorf("initialize handshake: %w", err)
	}

	if initResp.Error != nil {
		_ = c.Close()
		return fmt.Errorf("initialize handshake failed: %s (code: %d)", initResp.Error.Message, initResp.Error.Code)
	}

	// Send notifications/initialized
	initializedNotif := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	if err := c.sendNotification(ctx, initializedNotif); err != nil {
		_ = c.Close()
		return fmt.Errorf("send initialized notification: %w", err)
	}

	c.pendingMu.Lock()
	c.initialized = true
	c.pendingMu.Unlock()

	return nil
}

func (c *SSEClient) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	id := atomic.AddInt64(&c.nextID, 1)
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/list",
	}

	resp, err := c.sendRequest(ctx, id, req)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("list tools failed: %s (code: %d)", resp.Error.Message, resp.Error.Code)
	}

	var result ListToolsResult
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("unmarshal list tools result: %w", err)
	}

	return result.Tools, nil
}

func (c *SSEClient) CallTool(ctx context.Context, name string, arguments map[string]interface{}) (*CallToolResult, error) {
	id := atomic.AddInt64(&c.nextID, 1)
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/call",
		Params: CallToolParams{
			Name:      name,
			Arguments: arguments,
		},
	}

	resp, err := c.sendRequest(ctx, id, req)
	if err != nil {
		return nil, fmt.Errorf("call tool %s: %w", name, err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("call tool %s failed: %s (code: %d)", name, resp.Error.Message, resp.Error.Code)
	}

	var result CallToolResult
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("unmarshal call tool result: %w", err)
	}

	return &result, nil
}

func (c *SSEClient) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)

		c.pendingMu.Lock()
		for id, ch := range c.pending {
			close(ch)
			delete(c.pending, id)
		}
		c.pendingMu.Unlock()

		if c.sseBody != nil {
			_ = c.sseBody.Close()
		}
	})
	return nil
}

func (c *SSEClient) sendRequest(ctx context.Context, id interface{}, req JSONRPCRequest) (*JSONRPCResponse, error) {
	idStr := NormalizeID(id)
	ch := make(chan *JSONRPCResponse, 1)
	c.pendingMu.Lock()
	c.pending[idStr] = ch
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, idStr)
		c.pendingMu.Unlock()
	}()

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	c.postURLMu.RLock()
	postURL := c.postURL
	c.postURLMu.RUnlock()

	if postURL == "" {
		return nil, fmt.Errorf("post URL is not established")
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", postURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http post failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http post returned status %d: %s", resp.StatusCode, string(body))
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, fmt.Errorf("client closed")
	case r, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("response channel closed")
		}
		return r, nil
	}
}

func (c *SSEClient) sendNotification(ctx context.Context, req JSONRPCRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	c.postURLMu.RLock()
	postURL := c.postURL
	c.postURLMu.RUnlock()

	if postURL == "" {
		return fmt.Errorf("post URL is not established")
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", postURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("notification post returned status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (c *SSEClient) readSSELoop(body io.ReadCloser, endpointChan chan string) {
	scanner := bufio.NewScanner(body)
	var eventType string
	var dataBuffer strings.Builder

	for {
		select {
		case <-c.done:
			return
		default:
			if !scanner.Scan() {
				err := scanner.Err()
				if err != nil && err != io.EOF {
					slog.Error("MCP SSE Client read error", "error", err)
				}
				_ = c.Close()
				return
			}

			line := scanner.Text()
			if line == "" {
				// Empty line signals end of event, process it
				c.handleSSEEvent(eventType, dataBuffer.String(), endpointChan)
				eventType = ""
				dataBuffer.Reset()
				continue
			}

			if strings.HasPrefix(line, "event:") {
				eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				dataBuffer.WriteString(strings.TrimPrefix(line, "data:"))
			}
		}
	}
}

func (c *SSEClient) handleSSEEvent(eventType, data string, endpointChan chan string) {
	data = strings.TrimSpace(data)
	if data == "" {
		return
	}

	switch eventType {
	case "endpoint":
		select {
		case endpointChan <- data:
		default:
		}
	case "message", "":
		var resp JSONRPCResponse
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			slog.Error("MCP SSE Client failed to parse JSON-RPC response", "error", err, "raw", data)
			return
		}

		if resp.ID != nil {
			idStr := NormalizeID(resp.ID)
			c.pendingMu.Lock()
			ch, ok := c.pending[idStr]
			if ok {
				ch <- &resp
			}
			c.pendingMu.Unlock()
		}
	default:
		slog.Debug("MCP SSE Client received unknown event type", "type", eventType, "data", data)
	}
}
