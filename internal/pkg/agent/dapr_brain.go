package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/app/config"
	"github.com/qtopie/domour/internal/pkg/brain/diencephalon"
)

type daprBrainClient struct {
	sidecarHTTPAddress string
	appID              string
	httpClient         *http.Client
}

const (
	brainInvokeTimeout = 5 * time.Minute
)

func newDaprBrainClient(cfg config.DomourConfig) (BrainClient, error) {
	appID := strings.TrimSpace(cfg.ServiceAppID("brain"))
	if appID == "" {
		return nil, fmt.Errorf("dapr brain app id is empty")
	}
	return &daprBrainClient{
		sidecarHTTPAddress: cfg.DaprHTTPAddress(),
		appID:              appID,
		httpClient: &http.Client{
			Timeout: brainInvokeTimeout,
		},
	}, nil
}

type daprBrainReadyClient struct {
	provider string
	model    string
	c        *daprBrainClient
}

func (m *daprBrainReadyClient) Provider() string { return m.provider }
func (m *daprBrainReadyClient) Model() string    { return m.model }
func (m *daprBrainReadyClient) IsReady(ctx context.Context) (bool, error) {
	target := fmt.Sprintf(
		"http://%s/v1.0/invoke/%s/method/healthz",
		strings.TrimSpace(m.c.sidecarHTTPAddress),
		url.PathEscape(m.c.appID),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false, err
	}
	resp, err := m.c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("ping remote brain sidecar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("remote brain health check returned status %d", resp.StatusCode)
	}
	return true, nil
}

func (m *daprBrainReadyClient) GenerateMessage(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	return nil, fmt.Errorf("unimplemented in proxy client")
}
func (m *daprBrainReadyClient) GenerateText(ctx context.Context, messages []*schema.Message) (diencephalon.Response, error) {
	return diencephalon.Response{}, fmt.Errorf("unimplemented in proxy client")
}
func (m *daprBrainReadyClient) BindTools(tools []*schema.ToolInfo) error {
	return fmt.Errorf("unimplemented in proxy client")
}

func (c *daprBrainClient) GetClient(ctx context.Context, entry string) (diencephalon.Client, error) {
	domourCfg, err := config.LoadDomourConfig()
	if err != nil {
		return nil, err
	}
	resolvedCfg := diencephalon.ResolveConfig(entry, domourCfg)
	return &daprBrainReadyClient{
		provider: resolvedCfg.Provider,
		model:    resolvedCfg.Model,
		c:        c,
	}, nil
}

func (c *daprBrainClient) StreamChat(ctx context.Context, req BrainChatRequest) (<-chan BrainStreamEvent, error) {
	stream := make(chan BrainStreamEvent, 8)

	go func() {
		defer close(stream)

		if isDiagramLike(req.Message, req.Filename) {
			plan, err := c.PlanDiagram(ctx, BrainDiagramRequest{
				Workspace:     req.Workspace,
				Message:       req.Message,
				Filename:      req.Filename,
				FrontPart:     req.FrontPart,
				BackPart:      req.BackPart,
				Attachments:   req.Attachments,
				History:       req.History,
				MemorySummary: req.MemorySummary,
			})
			if err != nil {
				stream <- BrainStreamEvent{Type: "error", Err: err}
				return
			}
			stream <- BrainStreamEvent{
				Type:     "plan",
				Summary:  plan.Summary,
				Route:    plan.Route,
				Format:   plan.Format,
				Title:    plan.Title,
				Diagram:  plan.Diagram,
				Provider: plan.Provider,
				Model:    plan.Model,
			}
			return
		}

		reply, err := c.ChatReply(ctx, req)
		if err != nil {
			stream <- BrainStreamEvent{Type: "error", Err: err}
			return
		}
		for _, chunk := range splitReplyChunks(reply.Content) {
			select {
			case <-ctx.Done():
				return
			case stream <- BrainStreamEvent{
				Type:     "reply_chunk",
				Content:  chunk,
				Provider: reply.Provider,
				Model:    reply.Model,
			}:
			}
		}
		select {
		case <-ctx.Done():
			return
		case stream <- BrainStreamEvent{
			Type:     "reply_done",
			Provider: reply.Provider,
			Model:    reply.Model,
		}:
		}
	}()

	return stream, nil
}

func (c *daprBrainClient) StreamAutopilot(ctx context.Context, req BrainAutopilotRequest) (<-chan BrainStreamEvent, error) {
	stream := make(chan BrainStreamEvent, 8)

	go func() {
		defer close(stream)

		reply, err := c.Autopilot(ctx, req)
		if err != nil {
			stream <- BrainStreamEvent{Type: "error", Err: err}
			return
		}
		for _, chunk := range splitReplyChunks(reply.Content) {
			select {
			case <-ctx.Done():
				return
			case stream <- BrainStreamEvent{
				Type:     "autopilot_chunk",
				Content:  chunk,
				Provider: reply.Provider,
				Model:    reply.Model,
			}:
			}
		}
		select {
		case <-ctx.Done():
			return
		case stream <- BrainStreamEvent{
			Type:     "autopilot_done",
			Provider: reply.Provider,
			Model:    reply.Model,
		}:
		}
	}()

	return stream, nil
}

func (c *daprBrainClient) StreamCopilot(ctx context.Context, req BrainCopilotRequest) (<-chan BrainStreamEvent, error) {
	stream := make(chan BrainStreamEvent, 8)

	go func() {
		defer close(stream)

		reply, err := c.Copilot(ctx, req)
		if err != nil {
			stream <- BrainStreamEvent{Type: "error", Err: err}
			return
		}
		for _, chunk := range splitReplyChunks(reply.Content) {
			select {
			case <-ctx.Done():
				return
			case stream <- BrainStreamEvent{
				Type:     "copilot_chunk",
				Content:  chunk,
				Provider: reply.Provider,
				Model:    reply.Model,
			}:
			}
		}
		select {
		case <-ctx.Done():
			return
		case stream <- BrainStreamEvent{
			Type:     "copilot_done",
			Provider: reply.Provider,
			Model:    reply.Model,
		}:
		}
	}()

	return stream, nil
}

func (c *daprBrainClient) ChatReply(ctx context.Context, req BrainChatRequest) (BrainTextResponse, error) {
	var resp BrainTextResponse
	err := c.invoke(ctx, "/internal/brain/chat-reply", req, &resp)
	return resp, err
}

func (c *daprBrainClient) PlanDiagram(ctx context.Context, req BrainDiagramRequest) (BrainDiagramResponse, error) {
	var resp BrainDiagramResponse
	err := c.invoke(ctx, "/internal/brain/plan-diagram", req, &resp)
	return resp, err
}

func (c *daprBrainClient) Copilot(ctx context.Context, req BrainCopilotRequest) (BrainTextResponse, error) {
	var resp BrainTextResponse
	err := c.invoke(ctx, "/internal/brain/copilot", req, &resp)
	return resp, err
}

func (c *daprBrainClient) Autopilot(ctx context.Context, req BrainAutopilotRequest) (BrainTextResponse, error) {
	var resp BrainTextResponse
	err := c.invoke(ctx, "/internal/brain/autopilot", req, &resp)
	return resp, err
}

func (c *daprBrainClient) invoke(ctx context.Context, method string, reqBody, respBody any) error {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("encode dapr brain request: %w", err)
	}

	target := fmt.Sprintf(
		"http://%s/v1.0/invoke/%s/method%s",
		strings.TrimSpace(c.sidecarHTTPAddress),
		url.PathEscape(c.appID),
		method,
	)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build dapr brain request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("invoke dapr brain %s: %w", method, err)
	}
	defer resp.Body.Close()

	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("read dapr brain response %s: %w", method, readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("dapr brain %s returned %s: %s", method, resp.Status, strings.TrimSpace(string(data)))
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, respBody); err != nil {
		return fmt.Errorf("decode dapr brain response %s: %w", method, err)
	}
	return nil
}
