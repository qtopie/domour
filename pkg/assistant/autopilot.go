package assistant

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/cognitor/proxy"
	"github.com/qtopie/domour/internal/infra/llm"
)

func (s *AssistantService) Autopilot(ctx context.Context, req shared.MotorAutopilotRequest, provider, model string, attachments []shared.BrainAttachment) (shared.MotorAutopilotResponse, error) {
	sessionID := req.SessionID

	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return shared.MotorAutopilotResponse{}, fmt.Errorf("get session: %w", err)
	}

	// Session stickiness: prefer using the session's active provider if healthy and no override is specified
	if provider == "" && sess.ActiveProvider != "" {
		if proxy.IsProviderHealthy(sess.ActiveProvider) {
			provider = sess.ActiveProvider
			model = sess.ActiveModel
		}
	}

	// Resolve Brain LLM Client
	brainClient, err := s.engine.Cognitor().GetClientWithOverride(ctx, "autopilot", provider, model)
	if err != nil {
		return shared.MotorAutopilotResponse{}, fmt.Errorf("get brain client: %w", err)
	}

	// Check provider readiness
	if ready, readyErr := brainClient.IsReady(ctx); !ready || readyErr != nil {
		if readyErr != nil {
			return shared.MotorAutopilotResponse{}, readyErr
		}
		return shared.MotorAutopilotResponse{}, fmt.Errorf("provider %s is not ready", brainClient.Provider())
	}

	goal := strings.TrimSpace(req.Goal)
	if goal == "" {
		goal = "Clarify the user goal before running automation."
	}
	_ = s.AppendHistory(ctx, sessionID, "user", goal)

	// Safety Check / Veto on the goal
	if s.engine.Executor().Veto(ctx, goal) {
		return shared.MotorAutopilotResponse{
			Status: "refused",
			Result: "Motor refused to execute or return this result because it appears unsafe.",
			Meta: map[string]string{
				"policy":   "safety",
				"provider": "local-motor",
			},
		}, nil
	}

	// If it's a simple info check task, run simple direct response
	if isSimpleAutopilot(goal, req.Constraints, req.MaxSteps) {
		resultText := buildSimpleAutopilotResult(goal, req.Workspace, req.Constraints, req.MaxSteps)
		_ = s.AppendHistoryWithMeta(ctx, sessionID, "assistant", resultText, "local-motor", "direct")
		return shared.MotorAutopilotResponse{
			Status: "completed",
			Result: resultText,
			Meta: map[string]string{
				"provider": "local-motor",
				"mode":     "direct",
			},
		}, nil
	}

	// Dynamic cognitive reasoning execution
	messages := []*schema.Message{
		schema.SystemMessage("You are Domour Autopilot. Design and execute the target goals step-by-step using tools. Do not hesitate to check system logs or files to confirm state before concluding."),
	}
	messages = append(messages, llm.HistoryToSchema(sess.History, sess.MemorySummary)...)
	userMsg, err := llm.BuildUserInputMessage(
		BuildAutopilotPrompt(goal, req.Workspace, req.Constraints, req.MaxSteps),
		attachments,
	)
	if err != nil {
		return shared.MotorAutopilotResponse{}, fmt.Errorf("build autopilot prompt: %w", err)
	}
	messages = append(messages, userMsg)

	respMsg, err := s.runToolCallingLoop(ctx, brainClient, messages, func(event shared.MotorStreamEvent) error { return nil }, false, "")
	if err != nil {
		return shared.MotorAutopilotResponse{}, fmt.Errorf("run tool calling loop: %w", err)
	}
	resp := proxy.Response{
		Content:  respMsg.Content,
		Provider: brainClient.Provider(),
		Model:    brainClient.Model(),
	}

	// Safety check / Veto on generated plan/response
	if s.engine.Executor().Veto(ctx, resp.Content) {
		return shared.MotorAutopilotResponse{
			Status: "refused",
			Result: "Motor refused to execute or return this result because it appears unsafe.",
			Meta: map[string]string{
				"policy":   "safety",
				"provider": resp.Provider,
				"model":    resp.Model,
			},
		}, nil
	}

	_ = s.AppendHistoryWithMeta(ctx, sessionID, "assistant", resp.Content, resp.Provider, resp.Model)

	return shared.MotorAutopilotResponse{
		Status: "completed",
		Result: resp.Content,
		Meta: map[string]string{
			"provider": resp.Provider,
			"model":    resp.Model,
			"mode":     "brain-assisted",
		},
	}, nil
}

func isSimpleAutopilot(goal string, constraints []string, maxSteps int32) bool {
	goal = strings.ToLower(strings.TrimSpace(goal))
	if len(constraints) > 2 {
		return false
	}
	if maxSteps > 0 && maxSteps <= 3 {
		return true
	}

	for _, marker := range []string{
		"列出",
		"总结",
		"总结一下",
		"检查",
		"查看",
		"explain",
		"summarize",
		"list",
		"inspect",
		"check",
	} {
		if strings.Contains(goal, marker) {
			return true
		}
	}
	return false
}

func buildSimpleAutopilotResult(goal, workspace string, constraints []string, maxSteps int32) string {
	steps := []string{
		fmt.Sprintf("Goal: %s", goal),
		fmt.Sprintf("Workspace: %s", FirstNonEmpty(strings.TrimSpace(workspace), "not provided")),
	}
	if len(constraints) == 0 {
		steps = append(steps, "Constraints: none provided")
	} else {
		steps = append(steps, "Constraints: "+strings.Join(constraints, "; "))
	}

	plan := []string{
		"1. Clarify the expected deliverable and success criteria.",
		"2. Inspect the most relevant files, services, or runtime state.",
		"3. Return the minimal result directly without escalating to deeper planning.",
	}
	if maxSteps > 0 && int(maxSteps) < len(plan) {
		plan = plan[:maxSteps]
	}
	return strings.Join(append(steps, plan...), "\n")
}
