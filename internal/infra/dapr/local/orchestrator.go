package local

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/bionic/tool"
	"github.com/qtopie/domour/internal/engine"
	"github.com/qtopie/domour/internal/infra/dapr"
	"github.com/qtopie/domour/internal/infra/eventbus"
)

// LocalOrchestrator implements the DurableAgentOrchestrator interface locally.
type LocalOrchestrator struct {
	eng      engine.Engine
	eb       eventbus.EventBus
	mu       sync.RWMutex
	states   map[string]*dapr.WorkflowState
	doneChan map[string]chan struct{}
}

func NewLocalOrchestrator(eng engine.Engine, eb eventbus.EventBus) *LocalOrchestrator {
	return &LocalOrchestrator{
		eng:      eng,
		eb:       eb,
		states:   make(map[string]*dapr.WorkflowState),
		doneChan: make(map[string]chan struct{}),
	}
}

// StartWorkflow starts a local workflow instance running the ReAct loop.
func (o *LocalOrchestrator) StartWorkflow(ctx context.Context, workflowID string, input any) (string, error) {
	wfInput, ok := input.(dapr.AgentWorkflowInput)
	if !ok {
		wfInputPtr, okPtr := input.(*dapr.AgentWorkflowInput)
		if !okPtr {
			return "", fmt.Errorf("invalid workflow input type: %T", input)
		}
		wfInput = *wfInputPtr
	}

	o.mu.Lock()
	o.states[workflowID] = &dapr.WorkflowState{Status: "running"}
	done := make(chan struct{})
	o.doneChan[workflowID] = done
	o.mu.Unlock()

	go func() {
		defer close(done)
		res, err := o.runReActLoop(context.Background(), workflowID, wfInput)

		o.mu.Lock()
		state := o.states[workflowID]
		if err != nil {
			state.Status = "failed"
			state.Err = err
		} else {
			state.Status = "completed"
			state.Result = res
		}
		o.mu.Unlock()

		var event shared.MotorStreamEvent
		if err != nil {
			event = shared.MotorStreamEvent{
				Stage: wfInput.Stage,
				Done:  true,
				Err:   err,
			}
		} else {
			event = shared.MotorStreamEvent{
				Stage: wfInput.Stage,
				Done:  true,
			}
		}
		eventData, _ := json.Marshal(event)
		_ = o.eb.Publish(context.Background(), fmt.Sprintf("agent/workflow/%s/stream", workflowID), eventData)
	}()

	return workflowID, nil
}

// GetWorkflowStatus retrieves the status and result of a local workflow.
func (o *LocalOrchestrator) GetWorkflowStatus(ctx context.Context, workflowID string) (*dapr.WorkflowState, error) {
	o.mu.RLock()
	state, ok := o.states[workflowID]
	o.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("workflow %s not found", workflowID)
	}

	return state, nil
}

func (o *LocalOrchestrator) runReActLoop(ctx context.Context, workflowID string, input dapr.AgentWorkflowInput) (*schema.Message, error) {
	brainClient, err := o.eng.Cognitor().GetClientWithOverride(ctx, input.Stage, input.Provider, input.Model)
	if err != nil {
		return nil, fmt.Errorf("get brain client: %w", err)
	}

	if ready, readyErr := brainClient.IsReady(ctx); !ready || readyErr != nil {
		if readyErr != nil {
			return nil, readyErr
		}
		return nil, fmt.Errorf("provider %s is not ready", brainClient.Provider())
	}

	yield := func(event shared.MotorStreamEvent) error {
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		return o.eb.Publish(ctx, fmt.Sprintf("agent/workflow/%s/stream", workflowID), data)
	}

	messages := input.Messages

	tools, err := o.eng.Executor().ListTools(ctx)
	toolMapping := make(map[string]string)
	if err == nil && len(tools) > 0 && modelSupportsTools(brainClient.Provider(), brainClient.Model()) {
		for _, t := range tools {
			sanitized := tool.SanitizeToolName(t.Name)
			toolMapping[sanitized] = t.Name
		}
		einoTools := tool.GetEinoToolSchemas(tools)
		if bindErr := brainClient.BindTools(einoTools); bindErr != nil {
			// Log/ignore bind errors
		}
	}

	for loop := 0; loop < 10; loop++ {
		sr, err := brainClient.Chat.Stream(ctx, messages)
		if err != nil {
			return nil, err
		}
		if sr == nil {
			return nil, fmt.Errorf("model client returned nil StreamReader")
		}

		var chunks []*schema.Message
		isToolCall := false

		for {
			chunk, recvErr := sr.Recv()
			if recvErr == io.EOF {
				break
			}
			if recvErr != nil {
				sr.Close()
				return nil, recvErr
			}

			chunks = append(chunks, chunk)

			if len(chunk.ToolCalls) > 0 {
				isToolCall = true
			}

			if input.StreamFinal && !isToolCall {
				meta := map[string]string{"provider": brainClient.Provider(), "model": brainClient.Model()}
				if chunk.ReasoningContent != "" {
					if err := yield(shared.MotorStreamEvent{
						Stage:   input.Stage,
						Type:    1, // CHUNK_THINKING
						Content: chunk.ReasoningContent,
						Thinking: &shared.ThinkingDetail{
							Engine: brainClient.Provider(),
							Stage:  "thought",
						},
						Meta: meta,
					}); err != nil {
						return nil, err
					}
				}
				if chunk.Content != "" {
					if err := yield(shared.MotorStreamEvent{
						Stage:   input.Stage,
						Content: chunk.Content,
						Meta:    meta,
						// Type 0 = CHUNK_TEXT (proto3 default)
					}); err != nil {
						return nil, err
					}
				}
			}
		}
		sr.Close()

		respMsg, err := schema.ConcatMessages(chunks)
		if err != nil {
			return nil, fmt.Errorf("concat message chunks: %w", err)
		}

		if len(respMsg.ToolCalls) == 0 {
			return respMsg, nil
		}

		messages = append(messages, respMsg)

		for _, tc := range respMsg.ToolCalls {
			originalName := tc.Function.Name
			if mappedName, ok := toolMapping[tc.Function.Name]; ok {
				originalName = mappedName
			}

			_ = yield(shared.MotorStreamEvent{
				Stage:   "motor",
				Type:    2, // CHUNK_TOOL_CALL
				Content: fmt.Sprintf("Calling tool %q with args: %s\n", originalName, tc.Function.Arguments),
				Done:    false,
				Meta:    map[string]string{"tool": originalName},
				ToolCall: &shared.ToolCallDetail{
					ToolName:  originalName,
					ToolID:    tc.ID,
					Status:    "started",
					Arguments: tc.Function.Arguments,
				},
			})

			var args map[string]interface{}
			var observation string

			startTime := time.Now()
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				observation = fmt.Sprintf("Error parsing tool arguments: %v", err)
			} else {
				cmd := tool.Command{
					ID:     tc.ID,
					Action: originalName,
					Input:  args,
				}
				res, err := o.eng.Executor().Execute(ctx, cmd)
				if err != nil {
					observation = fmt.Sprintf("Tool execution failed: %v", err)
				} else {
					observation = res.Observation
				}
			}
			duration := time.Since(startTime).Milliseconds()

			_ = yield(shared.MotorStreamEvent{
				Stage:   "motor",
				Type:    2, // CHUNK_TOOL_CALL
				Content: fmt.Sprintf("Tool %q observation:\n%s\n", originalName, observation),
				Done:    false,
				Meta:    map[string]string{"tool": originalName},
				ToolCall: &shared.ToolCallDetail{
					ToolName:    originalName,
					ToolID:      tc.ID,
					Status:      "completed",
					Arguments:   tc.Function.Arguments,
					Observation: observation,
					DurationMs:  duration,
				},
			})

			toolMsg := &schema.Message{
				Role:       schema.Tool,
				Content:    observation,
				ToolCallID: tc.ID,
			}
			messages = append(messages, toolMsg)
		}
	}

	return nil, fmt.Errorf("max tool execution loops reached")
}

func modelSupportsTools(provider, model string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.ToLower(strings.TrimSpace(model))
	if provider == "ollama" {
		if strings.Contains(model, "smollm") ||
			strings.Contains(model, "tinyllama") ||
			strings.Contains(model, "phi-2") ||
			strings.Contains(model, "gemma:2b") ||
			strings.Contains(model, "qwen") && (strings.Contains(model, "0.5b") || strings.Contains(model, "1.5b") || strings.Contains(model, "1_8b")) {
			return false
		}
	}
	return true
}

