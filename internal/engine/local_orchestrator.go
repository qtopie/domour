package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/bionic/tool"
	"github.com/qtopie/domour/internal/cognitor/proxy"
	"github.com/qtopie/domour/internal/infra/eventbus"
)

var orchLog = slog.Default().With(slog.String("component", "LocalOrchestrator"))

// LocalOrchestrator implements the AgentOrchestrator interface locally in memory.
type LocalOrchestrator struct {
	cognitor CognitorClient
	executor ExecutorClient
	eb       eventbus.EventBus
	mu       sync.RWMutex
	states   map[string]*WorkflowState
	doneChan map[string]chan struct{}
}

// NewLocalOrchestrator creates a new local in-process orchestrator.
func NewLocalOrchestrator(cognitor CognitorClient, executor ExecutorClient, eb eventbus.EventBus) *LocalOrchestrator {
	return &LocalOrchestrator{
		cognitor: cognitor,
		executor: executor,
		eb:       eb,
		states:   make(map[string]*WorkflowState),
		doneChan: make(map[string]chan struct{}),
	}
}

func (o *LocalOrchestrator) EventBus() eventbus.EventBus {
	return o.eb
}

// StartWorkflow starts a local workflow instance running the specified reasoning strategy.
func (o *LocalOrchestrator) StartWorkflow(ctx context.Context, workflowID string, input any) (string, error) {
	wfInput, ok := input.(AgentWorkflowInput)
	if !ok {
		wfInputPtr, okPtr := input.(*AgentWorkflowInput)
		if !okPtr {
			return "", fmt.Errorf("invalid workflow input type: %T", input)
		}
		wfInput = *wfInputPtr
	}

	orchLog.Info("StartWorkflow",
		"workflow_id", workflowID,
		"provider", wfInput.Provider,
		"model", wfInput.Model,
		"stage", wfInput.Stage,
		"reasoning", wfInput.Reasoning,
		"message_count", len(wfInput.Messages),
	)

	o.mu.Lock()
	o.states[workflowID] = &WorkflowState{Status: "running"}
	done := make(chan struct{})
	o.doneChan[workflowID] = done
	o.mu.Unlock()

	go func() {
		defer close(done)
		res, err := o.runReasoningLoop(context.Background(), workflowID, wfInput)

		o.mu.Lock()
		state := o.states[workflowID]
		if err != nil {
			state.Status = "failed"
			state.Err = err
			orchLog.Error("Workflow failed", "workflow_id", workflowID, "error", err)
		} else {
			state.Status = "completed"
			state.Result = res
			orchLog.Info("Workflow completed", "workflow_id", workflowID)
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
func (o *LocalOrchestrator) GetWorkflowStatus(ctx context.Context, workflowID string) (*WorkflowState, error) {
	o.mu.RLock()
	state, ok := o.states[workflowID]
	o.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("workflow %s not found", workflowID)
	}

	return state, nil
}

// WaitForWorkflow blocks until the specified local workflow completes.
func (o *LocalOrchestrator) WaitForWorkflow(ctx context.Context, workflowID string) (*WorkflowState, error) {
	o.mu.RLock()
	done, ok := o.doneChan[workflowID]
	o.mu.RUnlock()

	if ok && done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return o.GetWorkflowStatus(ctx, workflowID)
}

// runReasoningLoop dispatches workflow execution to the configured reasoning strategy.
func (o *LocalOrchestrator) runReasoningLoop(ctx context.Context, workflowID string, input AgentWorkflowInput) (*schema.Message, error) {
	reasoningMode := strings.ToLower(strings.TrimSpace(input.Reasoning))
	switch reasoningMode {
	case "simple":
		return o.runSimpleLoop(ctx, workflowID, input)
	case "planner":
		return o.runPlannerLoop(ctx, workflowID, input)
	case "react", "":
		return o.runReActLoop(ctx, workflowID, input)
	default:
		orchLog.Info("runReasoningLoop: using default ReAct loop for reasoning mode", "mode", input.Reasoning)
		return o.runReActLoop(ctx, workflowID, input)
	}
}

// runSimpleLoop executes a single-turn reasoning strategy without ReAct tool loops.
func (o *LocalOrchestrator) runSimpleLoop(ctx context.Context, workflowID string, input AgentWorkflowInput) (*schema.Message, error) {
	if o.cognitor == nil {
		return nil, fmt.Errorf("cognitor client unavailable")
	}
	brainClient, err := o.cognitor.GetClientWithOverride(ctx, input.Stage, input.Provider, input.Model)
	if err != nil {
		return nil, fmt.Errorf("get brain client: %w", err)
	}
	return brainClient.Chat.Generate(ctx, input.Messages)
}

// runPlannerLoop executes a multi-step DAG planner reasoning strategy.
func (o *LocalOrchestrator) runPlannerLoop(ctx context.Context, workflowID string, input AgentWorkflowInput) (*schema.Message, error) {
	return o.runReActLoop(ctx, workflowID, input)
}

// runReActLoop executes the standard Reasoning + Acting (ReAct) tool-calling loop.
func (o *LocalOrchestrator) runReActLoop(ctx context.Context, workflowID string, input AgentWorkflowInput) (*schema.Message, error) {
	orchLog.Info("runReActLoop: resolving brain client",
		"workflow_id", workflowID,
		"requested_provider", input.Provider,
		"requested_model", input.Model,
	)
	if o.cognitor == nil {
		return nil, fmt.Errorf("cognitor client unavailable")
	}
	brainClient, err := o.cognitor.GetClientWithOverride(ctx, input.Stage, input.Provider, input.Model)
	if err != nil {
		orchLog.Error("runReActLoop: failed to get brain client", "workflow_id", workflowID, "error", err)
		return nil, fmt.Errorf("get brain client: %w", err)
	}

	orchLog.Info("runReActLoop: brain client resolved",
		"workflow_id", workflowID,
		"resolved_provider", brainClient.Provider(),
		"resolved_model", brainClient.Model(),
		"base_url", brainClient.BaseURL(),
	)

	if ready, readyErr := brainClient.IsReady(ctx); !ready || readyErr != nil {
		if readyErr != nil {
			orchLog.Error("runReActLoop: brain client not ready", "workflow_id", workflowID, "error", readyErr, "provider", brainClient.Provider())
			return nil, readyErr
		}
		orchLog.Error("runReActLoop: brain client not ready", "workflow_id", workflowID, "provider", brainClient.Provider())
		return nil, fmt.Errorf("provider %s is not ready", brainClient.Provider())
	}
	orchLog.Info("runReActLoop: brain client ready, starting stream", "workflow_id", workflowID)

	yield := func(event shared.MotorStreamEvent) error {
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		return o.eb.Publish(ctx, fmt.Sprintf("agent/workflow/%s/stream", workflowID), data)
	}

	messages := input.Messages

	var tools []tool.ToolInfo
	if o.executor != nil {
		tools, _ = o.executor.ListTools(ctx)
	}
	toolMapping := make(map[string]string)
	if len(tools) > 0 && modelSupportsTools(brainClient.Provider(), brainClient.Model()) {
		for _, t := range tools {
			sanitized := tool.SanitizeToolName(t.Name)
			toolMapping[sanitized] = t.Name
		}
		einoTools := tool.GetEinoToolSchemas(tools)
		if bindErr := brainClient.BindTools(einoTools); bindErr != nil {
			// Log/ignore bind errors
		}
	}

	const maxLoops = 25
	for loop := 0; loop < maxLoops; loop++ {
		if o.executor != nil {
			if toolMgr := o.executor.ToolManager(); toolMgr != nil {
				if activePrompt := toolMgr.ActiveSkillPrompt(); activePrompt != "" {
					for i, msg := range messages {
						if msg.Role == schema.System {
							messages[i] = schema.SystemMessage(msg.Content + "\n\n" + activePrompt)
							break
						}
					}
					toolMgr.ClearActiveSkillPrompt()
				}
			}
		}

		orchLog.Info("runReActLoop: calling Stream", "workflow_id", workflowID, "loop", loop, "message_count", len(messages))
		sr, err := brainClient.Chat.Stream(ctx, messages)
		if err != nil {
			orchLog.Error("runReActLoop: Stream call failed", "workflow_id", workflowID, "loop", loop, "error", err)
			if strings.Contains(err.Error(), "model output must contain either output text or tool calls") {
				return nil, fmt.Errorf("本地模型返回了空响应，这通常发生在模型正在加载或上下文过长时。请稍后再试。(model=%s)", brainClient.Model())
			}
			return nil, err
		}
		if sr == nil {
			return nil, fmt.Errorf("model client returned nil StreamReader")
		}
		orchLog.Info("runReActLoop: Stream started, receiving chunks", "workflow_id", workflowID, "loop", loop)

		var chunks []*schema.Message
		isToolCall := false

		thinkParser := proxy.NewThinkTagParser()

		for {
			chunk, recvErr := sr.Recv()
			if recvErr == io.EOF {
				break
			}
			if recvErr != nil {
				orchLog.Error("runReActLoop: recv error during streaming", "workflow_id", workflowID, "loop", loop, "error", recvErr)
				sr.Close()
				return nil, recvErr
			}

			chunks = append(chunks, chunk)

			if len(chunk.ToolCalls) > 0 {
				isToolCall = true
			}

			if input.StreamFinal && !isToolCall {
				if chunk.ReasoningContent != "" {
					meta := map[string]string{"provider": brainClient.Provider(), "model": brainClient.Model()}
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
					if err := thinkParser.Feed(chunk.Content, input.Stage, brainClient, yield); err != nil {
						return nil, err
					}
				}
			}
		}
		if input.StreamFinal && !isToolCall {
			if err := thinkParser.Flush(input.Stage, brainClient, yield); err != nil {
				return nil, err
			}
		}
		sr.Close()
		orchLog.Info("runReActLoop: stream done", "workflow_id", workflowID, "loop", loop, "chunks_received", len(chunks), "is_tool_call", isToolCall)

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
				var res tool.Result
				if o.executor != nil {
					res, err = o.executor.Execute(ctx, cmd)
				} else {
					err = fmt.Errorf("executor client unavailable")
				}
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

	return nil, fmt.Errorf("__brain_review__: max tool execution loops (%d) reached in session %s. "+
		"The workflow exceeded the maximum number of tool call iterations. "+
		"Please review the approach and re-plan with a different strategy.", maxLoops, input.SessionID)
}

func modelSupportsTools(provider, model string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.ToLower(strings.TrimSpace(model))
	if provider == "llamacpp" || provider == "ollama" {
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
