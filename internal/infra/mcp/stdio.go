package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

type StdioClient struct {
	command string
	args    []string
	env     map[string]string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	nextID     int64
	pendingMu  sync.Mutex
	pending    map[string]chan *JSONRPCResponse
	initialized bool

	closeOnce sync.Once
	done      chan struct{}
}

func NewStdioClient(command string, args []string, env map[string]string) *StdioClient {
	return &StdioClient{
		command:   command,
		args:      args,
		env:       env,
		pending:   make(map[string]chan *JSONRPCResponse),
		done:      make(chan struct{}),
	}
}

func (c *StdioClient) Initialize(ctx context.Context) error {
	c.pendingMu.Lock()
	if c.initialized {
		c.pendingMu.Unlock()
		return nil
	}
	c.pendingMu.Unlock()

	cmd := exec.CommandContext(ctx, c.command, c.args...)
	if len(c.env) > 0 {
		envSlice := os.Environ()
		for k, v := range c.env {
			envSlice = append(envSlice, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = envSlice
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return fmt.Errorf("start process: %w", err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stdout = stdout
	c.stderr = stderr

	// Spawn stderr logger
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			slog.Debug("MCP Server stderr", "server", c.command, "log", scanner.Text())
		}
	}()

	// Spawn stdout reader loop
	go c.readLoop()

	// Perform initialize handshake
	id := atomic.AddInt64(&c.nextID, 1)
	req := JSONRPCRequest{
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

	resp, err := c.sendRequest(ctx, id, req)
	if err != nil {
		_ = c.Close()
		return fmt.Errorf("initialize handshake: %w", err)
	}

	if resp.Error != nil {
		_ = c.Close()
		return fmt.Errorf("initialize handshake failed: %s (code: %d)", resp.Error.Message, resp.Error.Code)
	}

	// Send notifications/initialized
	initializedNotif := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	if err := c.sendNotification(initializedNotif); err != nil {
		_ = c.Close()
		return fmt.Errorf("send initialized notification: %w", err)
	}

	c.pendingMu.Lock()
	c.initialized = true
	c.pendingMu.Unlock()

	return nil
}

func (c *StdioClient) ListTools(ctx context.Context) ([]ToolDefinition, error) {
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

func (c *StdioClient) CallTool(ctx context.Context, name string, arguments map[string]interface{}) (*CallToolResult, error) {
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

func (c *StdioClient) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.done)

		c.pendingMu.Lock()
		for id, ch := range c.pending {
			close(ch)
			delete(c.pending, id)
		}
		c.pendingMu.Unlock()

		if c.stdin != nil {
			_ = c.stdin.Close()
		}

		if c.cmd != nil && c.cmd.Process != nil {
			// Kill the subprocess
			_ = c.cmd.Process.Kill()
			err = c.cmd.Wait()
		}
	})
	return err
}

func (c *StdioClient) sendRequest(ctx context.Context, id interface{}, req JSONRPCRequest) (*JSONRPCResponse, error) {
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

	c.pendingMu.Lock()
	if c.stdin == nil {
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("client not connected")
	}
	_, err = c.stdin.Write(append(data, '\n'))
	c.pendingMu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, fmt.Errorf("client closed")
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("response channel closed")
		}
		return resp, nil
	}
}

func (c *StdioClient) sendNotification(req JSONRPCRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	if c.stdin == nil {
		return fmt.Errorf("client not connected")
	}
	_, err = c.stdin.Write(append(data, '\n'))
	return err
}

func (c *StdioClient) readLoop() {
	reader := bufio.NewReader(c.stdout)
	for {
		select {
		case <-c.done:
			return
		default:
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err != io.EOF {
					slog.Error("MCP Stdio Client read error", "error", err)
				}
				_ = c.Close()
				return
			}

			var resp JSONRPCResponse
			if err := json.Unmarshal(line, &resp); err != nil {
				slog.Error("MCP Stdio Client failed to parse JSON-RPC response", "error", err, "raw", string(line))
				continue
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
		}
	}
}
