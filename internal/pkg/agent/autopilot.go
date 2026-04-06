package agent

import (
	"context"
	"fmt"
	"strings"

	autopilotpb "github.com/qtopie/domour/gen/assistant/autopilot"
)

func (s *Server) Autopilot(ctx context.Context, req *autopilotpb.AutopilotRequest) (*autopilotpb.AutopilotResponse, error) {
	sessionID := normalizeSessionID(req.GetSessionId())
	ctx = withRuntimeMetadata(ctx, sessionID, req.GetWorkspace())
	history, _ := s.getHistory(ctx, sessionID)
	goal := strings.TrimSpace(req.GetGoal())
	if goal == "" {
		goal = "Clarify the user goal before running automation."
	}

	_ = s.appendHistory(ctx, sessionID, "user", goal)
	brainCtx, brainCancel := context.WithCancel(ctx)
	defer brainCancel()

	result, err := s.motor.Autopilot(ctx, MotorAutopilotRequest{
		SessionID:    sessionID,
		Seq:          req.GetSeq(),
		Workspace:    req.GetWorkspace(),
		Goal:         goal,
		Constraints:  req.GetConstraints(),
		MaxSteps:     req.GetMaxSteps(),
		HistoryCount: len(history),
	}, func(bridge *SessionBridge) {
		go s.streamAutopilotBrainToBridge(brainCtx, brainCancel, BrainAutopilotRequest{
			Workspace:   req.GetWorkspace(),
			Goal:        goal,
			Constraints: req.GetConstraints(),
			MaxSteps:    req.GetMaxSteps(),
			History:     history,
		}, bridge)
	})
	if err != nil {
		return nil, err
	}
	_ = s.appendHistory(ctx, sessionID, "assistant", result.Result)

	return &autopilotpb.AutopilotResponse{
		SessionId: sessionID,
		Seq:       req.GetSeq(),
		Status:    result.Status,
		Result:    result.Result,
		Meta:      mergeAutopilotMeta(result.Meta),
	}, nil
}

func buildAutopilotPrompt(goal, workspace string, constraints []string, maxSteps int32) string {
	parts := []string{
		"Goal: " + goal,
		"Workspace: " + firstNonEmpty(strings.TrimSpace(workspace), "not provided"),
	}
	if len(constraints) > 0 {
		parts = append(parts, "Constraints: "+strings.Join(constraints, "; "))
	} else {
		parts = append(parts, "Constraints: none provided")
	}
	if maxSteps > 0 {
		parts = append(parts, fmt.Sprintf("Max steps: %d", maxSteps))
	}
	return strings.Join(parts, "\n")
}

func mergeAutopilotMeta(meta map[string]string) map[string]string {
	out := map[string]string{
		"entry": "autopilot",
		"mode":  "mvp",
	}
	for k, v := range meta {
		out[k] = v
	}
	return out
}
