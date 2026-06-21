package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/cognitor/proxy"
	"github.com/qtopie/domour/internal/infra/dapr"
	"github.com/qtopie/domour/internal/infra/llm"
)

func (s *AssistantService) Copilot(ctx context.Context, req shared.MotorCopilotRequest, provider, model string, attachments []shared.BrainAttachment, yield func(event shared.MotorStreamEvent) error) error {
	sessionID := req.SessionID

	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	// Session stickiness: prefer using the session's active provider if healthy and no override is specified
	if provider == "" && sess.ActiveProvider != "" {
		if proxy.IsProviderHealthy(sess.ActiveProvider) {
			provider = sess.ActiveProvider
			model = sess.ActiveModel
		}
	}

	// Resolve Brain LLM Client
	brainClient, err := s.engine.Cognitor().GetClientWithOverride(ctx, "copilot", provider, model)
	if err != nil {
		return fmt.Errorf("get brain client: %w", err)
	}

	// Check provider readiness
	if ready, readyErr := brainClient.IsReady(ctx); !ready || readyErr != nil {
		if readyErr != nil {
			return readyErr
		}
		return fmt.Errorf("provider %s is not ready", brainClient.Provider())
	}

	userMessage := strings.TrimSpace(req.Message)
	if userMessage == "" {
		userMessage = "Please describe the change you want to make."
	}
	_ = s.AppendHistory(ctx, sessionID, "user", userMessage)

	mode := req.Mode

	// Veto on input
	if s.engine.Executor().Veto(ctx, userMessage) {
		return yieldCopilotRefusal(yield, brainClient.Provider(), brainClient.Model(), mode)
	}

	// For normal mode, if it's a simple request, run simple response directly
	if mode != "active" && isSimpleCopilot(userMessage, req.Filename, req.CodeBefore, req.CodeAfter) {
		resultText := buildSimpleCopilotResult(userMessage)
		if err := yield(shared.MotorStreamEvent{
			Stage:   "copilot",
			Content: resultText,
			Done:    true,
			Meta:    map[string]string{"provider": "local-motor", "mode": "normal"},
		}); err != nil {
			return err
		}
		_ = s.AppendHistoryWithMeta(ctx, sessionID, "assistant", resultText, "local-motor", "direct")
		return nil
	}

	// Build Copilot Generation prompt
	messages := []*schema.Message{
		schema.SystemMessage("You are Domour Copilot. Produce the smallest correct patch or code suggestion for the user's request. Prefer concrete code over high-level advice when enough context is present."),
	}
	messages = append(messages, llm.HistoryToSchema(sess.History, sess.MemorySummary)...)
	userMsg, err := llm.BuildUserInputMessage(
		BuildCopilotPrompt(userMessage, req.Workspace, req.Filename, req.CodeBefore, req.CodeAfter, req.CursorOffset),
		attachments,
	)
	if err != nil {
		return fmt.Errorf("build copilot prompt: %w", err)
	}
	messages = append(messages, userMsg)

	workflowID := fmt.Sprintf("copilot-%s-%d", sessionID, req.Seq)
	topic := fmt.Sprintf("agent/workflow/%s/stream", workflowID)
	doneChan := make(chan struct{})
	var finalErr error

	sub, err := s.eb.Subscribe(ctx, topic, func(data []byte) {
		var event shared.MotorStreamEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return
		}
		_ = yield(event)
		if event.Done {
			if event.Err != nil {
				finalErr = event.Err
			}
			close(doneChan)
		}
	})
	if err != nil {
		return fmt.Errorf("subscribe stream: %w", err)
	}

	input := dapr.AgentWorkflowInput{
		SessionID:   sessionID,
		Messages:    messages,
		Provider:    brainClient.Provider(),
		Model:       brainClient.Model(),
		StreamFinal: true,
		Stage:       "copilot",
	}

	_, err = s.orchestrator.StartWorkflow(ctx, workflowID, input)
	if err != nil {
		sub.Unsubscribe()
		return fmt.Errorf("start agent workflow: %w", err)
	}

	select {
	case <-doneChan:
	case <-ctx.Done():
		sub.Unsubscribe()
		return ctx.Err()
	}
	sub.Unsubscribe()

	if finalErr != nil {
		return fmt.Errorf("workflow execution failed: %w", finalErr)
	}

	wfStatus, err := s.orchestrator.GetWorkflowStatus(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("get workflow status: %w", err)
	}

	resp := proxy.Response{
		Content:  wfStatus.Result.Content,
		Provider: brainClient.Provider(),
		Model:    brainClient.Model(),
	}

	// Veto on output
	if s.engine.Executor().Veto(ctx, resp.Content) {
		return yieldCopilotRefusal(yield, resp.Provider, resp.Model, mode)
	}

	// Stream final done message
	if err := yield(shared.MotorStreamEvent{
		Stage:   "copilot",
		Content: "",
		Done:    true,
		Meta:    map[string]string{"provider": resp.Provider, "model": resp.Model, "mode": mode},
	}); err != nil {
		return err
	}

	_ = s.AppendHistoryWithMeta(ctx, sessionID, "assistant", resp.Content, resp.Provider, resp.Model)
	return nil
}

func yieldCopilotRefusal(yield func(event shared.MotorStreamEvent) error, provider, model, mode string) error {
	refusal := "Motor refused to execute or return this result because it appears unsafe."
	return yield(shared.MotorStreamEvent{
		Stage:   "motor",
		Content: refusal,
		Done:    true,
		Meta: map[string]string{
			"provider": provider,
			"model":    model,
			"policy":   "safety",
			"mode":     mode,
		},
	})
}

func isSimpleCopilot(message, filename, before, after string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if filename != "" && (before != "" || after != "") {
		return false
	}
	for _, marker := range []string{"rename", "comment", "summarize", "explain", "解释", "总结", "说明"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func buildSimpleCopilotResult(message string) string {
	return strings.Join([]string{
		"Motor handled this copilot request directly.",
		"Request: " + FirstNonEmpty(strings.TrimSpace(message), "Describe the change."),
		"Suggested flow:",
		"1. Confirm the target file and scope.",
		"2. Make the smallest safe change.",
		"3. Explain the expected effect and any follow-up checks.",
	}, "\n")
}
