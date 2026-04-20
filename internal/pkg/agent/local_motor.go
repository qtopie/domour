package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/qtopie/domour/internal/pkg/motor"
)

type localMotorClient struct {
	manager     *motor.Manager
	interceptor chatContextInterceptor
}

func newLocalMotorClient() (MotorClient, error) {
	manager, err := motor.NewDefaultManager()
	if err != nil {
		return nil, err
	}
	return &localMotorClient{
		manager:     manager,
		interceptor: newChatContextInterceptor(),
	}, nil
}

func (m *localMotorClient) StreamChat(ctx context.Context, req MotorChatRequest, bridge *SessionBridge) error {
	defer close(bridge.MotorOut)
	var replyParts []string
	go m.trySendChatInterception(ctx, req)

	for {
		select {
		case <-ctx.Done():
			bridge.MotorOut <- MotorStreamEvent{Stage: "error", Err: ctx.Err()}
			return ctx.Err()
		case event, ok := <-bridge.BrainOut:
			if !ok {
				return nil
			}
			if event.Err != nil {
				bridge.MotorOut <- MotorStreamEvent{Stage: "error", Err: event.Err}
				return event.Err
			}

			switch event.Type {
			case "reply_chunk":
				if shouldRefuseOutput(req.Message, strings.Join(append(replyParts, event.Content), "\n")) {
					sendBrainControl(bridge, BrainControl{
						Type:    "refuse",
						Content: "motor refused unsafe reply output",
						Meta:    map[string]string{"reason": "safety"},
					})
					bridge.MotorOut <- MotorStreamEvent{
						Stage:   "motor",
						Content: buildRefusalReply(),
						Done:    true,
						Meta: map[string]string{
							"provider": event.Provider,
							"model":    event.Model,
							"policy":   "safety",
						},
					}
					return nil
				}

				replyParts = append(replyParts, event.Content)
				bridge.MotorOut <- MotorStreamEvent{
					Stage:   "reply",
					Content: event.Content,
					Done:    false,
					Meta: map[string]string{
						"provider": event.Provider,
						"model":    event.Model,
					},
				}
			case "reply_done":
				bridge.MotorOut <- MotorStreamEvent{
					Stage:   "reply",
					Content: "",
					Done:    true,
					Meta: map[string]string{
						"provider": event.Provider,
						"model":    event.Model,
					},
				}
				return nil
			case "plan":
				if shouldRefuseOutput(req.Message, event.Diagram) {
					sendBrainControl(bridge, BrainControl{
						Type:    "refuse",
						Content: "motor refused unsafe diagram plan",
						Meta:    map[string]string{"reason": "safety"},
					})
					bridge.MotorOut <- MotorStreamEvent{
						Stage:   "motor",
						Content: buildRefusalReply(),
						Done:    true,
						Meta: map[string]string{
							"format":   event.Format,
							"provider": event.Provider,
							"model":    event.Model,
							"policy":   "safety",
						},
					}
					return nil
				}

				bridge.MotorOut <- MotorStreamEvent{
					Stage:   "brain",
					Content: buildChatSummaryMessage(req.Message, req.Workspace, req.Filename, len(req.History), event.Summary),
					Done:    false,
					Meta: map[string]string{
						"format":   event.Format,
						"provider": event.Provider,
						"model":    event.Model,
					},
				}

				result, err := m.manager.Execute(ctx, motor.Command{
					ID:     fmt.Sprintf("chat-%d-render", req.Seq),
					Action: event.Route,
					Input: map[string]interface{}{
						"source": event.Diagram,
						"format": event.Format,
						"title":  event.Title,
					},
				})
				if err != nil {
					sendBrainControl(bridge, BrainControl{
						Type:    "stop",
						Content: "motor stopped after render failure",
						Meta:    map[string]string{"reason": "render_error"},
					})
					bridge.MotorOut <- MotorStreamEvent{Stage: "error", Err: err}
					return err
				}

				bridge.MotorOut <- MotorStreamEvent{
					Stage:   "motor",
					Content: buildRenderedReply(event.Diagram, result.Observation),
					Done:    true,
					Meta: map[string]string{
						"format":   result.Meta["format"],
						"provider": event.Provider,
						"model":    event.Model,
					},
				}
			}
		}
	}
}

func (m *localMotorClient) trySendChatInterception(ctx context.Context, req MotorChatRequest) {
	if m == nil || m.interceptor == nil || len(imageOnlyAttachments(req.Attachments)) == 0 {
		return
	}
	interception, err := m.interceptor.InterceptChatContext(ctx, req)
	if err != nil || interception == nil {
		return
	}
	defaultChatContextWorkingSet.Update(req.SessionID, req.Seq, interception)
}

func (m *localMotorClient) Autopilot(ctx context.Context, req MotorAutopilotRequest, startBrain func(*SessionBridge)) (MotorAutopilotResponse, error) {
	if shouldRefuseOutput(req.Goal, strings.Join(req.Constraints, "\n")) {
		return MotorAutopilotResponse{
			Status: "refused",
			Result: buildRefusalReply(),
			Meta: map[string]string{
				"policy":   "safety",
				"provider": "local-motor",
			},
		}, nil
	}

	if isSimpleAutopilot(req) {
		return MotorAutopilotResponse{
			Status: "completed",
			Result: buildSimpleAutopilotResult(req),
			Meta: map[string]string{
				"provider": "local-motor",
				"mode":     "direct",
			},
		}, nil
	}

	bridge := newSessionBridge()
	startBrain(bridge)

	var parts []string
	var provider, model string

	for {
		select {
		case <-ctx.Done():
			return MotorAutopilotResponse{}, ctx.Err()
		case event, ok := <-bridge.BrainOut:
			if !ok {
				return MotorAutopilotResponse{
					Status: "completed",
					Result: strings.Join(parts, "\n"),
					Meta: map[string]string{
						"provider": provider,
						"model":    model,
						"mode":     "brain-assisted",
					},
				}, nil
			}
			if event.Err != nil {
				return MotorAutopilotResponse{}, event.Err
			}

			switch event.Type {
			case "autopilot_chunk":
				if shouldRefuseOutput(req.Goal, strings.Join(append(parts, event.Content), "\n")) {
					sendBrainControl(bridge, BrainControl{
						Type:    "refuse",
						Content: "motor refused unsafe autopilot plan",
						Meta:    map[string]string{"reason": "safety"},
					})
					return MotorAutopilotResponse{
						Status: "refused",
						Result: buildRefusalReply(),
						Meta: map[string]string{
							"policy":   "safety",
							"provider": event.Provider,
							"model":    event.Model,
						},
					}, nil
				}
				parts = append(parts, event.Content)
				provider = event.Provider
				model = event.Model
			case "autopilot_done":
				return MotorAutopilotResponse{
					Status: "completed",
					Result: strings.Join(parts, "\n"),
					Meta: map[string]string{
						"provider": provider,
						"model":    model,
						"mode":     "brain-assisted",
					},
				}, nil
			}
		}
	}
}

func (m *localMotorClient) Copilot(ctx context.Context, req MotorCopilotRequest, startBrain func(*SessionBridge)) (<-chan MotorStreamEvent, error) {
	stream := make(chan MotorStreamEvent, 8)

	go func() {
		defer close(stream)

		switch req.Mode {
		case "active":
			bridge := newSessionBridge()
			startBrain(bridge)

			for {
				select {
				case <-ctx.Done():
					stream <- MotorStreamEvent{Stage: "error", Err: ctx.Err()}
					return
				case event, ok := <-bridge.BrainOut:
					if !ok {
						return
					}
					if event.Err != nil {
						stream <- MotorStreamEvent{Stage: "error", Err: event.Err}
						return
					}

					switch event.Type {
					case "copilot_chunk":
						if shouldRefuseOutput(req.Message, event.Content) {
							sendBrainControl(bridge, BrainControl{
								Type:    "refuse",
								Content: "motor refused unsafe copilot output",
								Meta:    map[string]string{"reason": "safety"},
							})
							stream <- MotorStreamEvent{
								Stage:   "motor",
								Content: buildRefusalReply(),
								Done:    true,
								Meta: map[string]string{
									"provider": event.Provider,
									"model":    event.Model,
									"policy":   "safety",
									"mode":     "active",
								},
							}
							return
						}
						stream <- MotorStreamEvent{
							Stage:   "copilot",
							Content: event.Content,
							Done:    false,
							Meta: map[string]string{
								"provider": event.Provider,
								"model":    event.Model,
								"mode":     "active",
							},
						}
					case "copilot_done":
						stream <- MotorStreamEvent{
							Stage:   "copilot",
							Content: "",
							Done:    true,
							Meta: map[string]string{
								"provider": event.Provider,
								"model":    event.Model,
								"mode":     "active",
							},
						}
						return
					}
				}
			}
		default:
			if shouldRefuseOutput(req.Message, req.CodeBefore+"\n"+req.CodeAfter) {
				stream <- MotorStreamEvent{
					Stage:   "motor",
					Content: buildRefusalReply(),
					Done:    true,
					Meta: map[string]string{
						"provider": "local-motor",
						"policy":   "safety",
						"mode":     "normal",
					},
				}
				return
			}

			if isSimpleCopilot(req) {
				stream <- MotorStreamEvent{
					Stage:   "copilot",
					Content: buildSimpleCopilotResult(req),
					Done:    true,
					Meta: map[string]string{
						"provider": "local-motor",
						"mode":     "normal",
					},
				}
				return
			}

			bridge := newSessionBridge()
			startBrain(bridge)
			var parts []string
			var provider, model string
			for {
				select {
				case <-ctx.Done():
					stream <- MotorStreamEvent{Stage: "error", Err: ctx.Err()}
					return
				case event, ok := <-bridge.BrainOut:
					if !ok {
						stream <- MotorStreamEvent{
							Stage:   "copilot",
							Content: strings.Join(parts, "\n"),
							Done:    true,
							Meta: map[string]string{
								"provider": provider,
								"model":    model,
								"mode":     "normal",
							},
						}
						return
					}
					if event.Err != nil {
						stream <- MotorStreamEvent{Stage: "error", Err: event.Err}
						return
					}
					switch event.Type {
					case "copilot_chunk":
						if shouldRefuseOutput(req.Message, strings.Join(append(parts, event.Content), "\n")) {
							sendBrainControl(bridge, BrainControl{
								Type:    "refuse",
								Content: "motor refused unsafe copilot patch",
								Meta:    map[string]string{"reason": "safety"},
							})
							stream <- MotorStreamEvent{
								Stage:   "motor",
								Content: buildRefusalReply(),
								Done:    true,
								Meta: map[string]string{
									"provider": event.Provider,
									"model":    event.Model,
									"policy":   "safety",
									"mode":     "normal",
								},
							}
							return
						}
						parts = append(parts, event.Content)
						provider = event.Provider
						model = event.Model
					case "copilot_done":
						stream <- MotorStreamEvent{
							Stage:   "copilot",
							Content: strings.Join(parts, "\n"),
							Done:    true,
							Meta: map[string]string{
								"provider": provider,
								"model":    model,
								"mode":     "normal",
							},
						}
						return
					}
				}
			}
		}
	}()

	return stream, nil
}

func (m *localMotorClient) Execute(ctx context.Context, command motor.Command) (motor.Result, error) {
	return m.manager.Execute(ctx, command)
}

func sendBrainControl(bridge *SessionBridge, control BrainControl) {
	select {
	case bridge.Control <- control:
	default:
	}
}

func shouldRefuseOutput(prompt, content string) bool {
	value := strings.ToLower(strings.TrimSpace(prompt + "\n" + content))
	for _, marker := range []string{
		"jump off",
		"kill myself",
		"suicide",
		"hurt myself",
		"伤害自己",
		"自杀",
		"跳楼",
		"坠楼",
		"往下坠",
		"伤害他人",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func buildRefusalReply() string {
	return "Motor refused to execute or return this result because it appears unsafe."
}

func isSimpleAutopilot(req MotorAutopilotRequest) bool {
	goal := strings.ToLower(strings.TrimSpace(req.Goal))
	if len(req.Constraints) > 2 {
		return false
	}
	if req.MaxSteps > 0 && req.MaxSteps <= 3 {
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

func buildSimpleAutopilotResult(req MotorAutopilotRequest) string {
	steps := []string{
		fmt.Sprintf("Goal: %s", req.Goal),
		fmt.Sprintf("Workspace: %s", firstNonEmpty(strings.TrimSpace(req.Workspace), "not provided")),
	}
	if len(req.Constraints) == 0 {
		steps = append(steps, "Constraints: none provided")
	} else {
		steps = append(steps, "Constraints: "+strings.Join(req.Constraints, "; "))
	}

	plan := []string{
		"1. Clarify the expected deliverable and success criteria.",
		"2. Inspect the most relevant files, services, or runtime state.",
		"3. Return the minimal result directly without escalating to deeper planning.",
	}
	if req.MaxSteps > 0 && int(req.MaxSteps) < len(plan) {
		plan = plan[:req.MaxSteps]
	}
	return strings.Join(append(steps, plan...), "\n")
}

func isSimpleCopilot(req MotorCopilotRequest) bool {
	msg := strings.ToLower(strings.TrimSpace(req.Message))
	if req.Filename != "" && (req.CodeBefore != "" || req.CodeAfter != "") {
		return false
	}
	for _, marker := range []string{"rename", "comment", "summarize", "explain", "解释", "总结", "说明"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func buildSimpleCopilotResult(req MotorCopilotRequest) string {
	return strings.Join([]string{
		"Motor handled this copilot request directly.",
		"Request: " + firstNonEmpty(strings.TrimSpace(req.Message), "Describe the change."),
		"Suggested flow:",
		"1. Confirm the target file and scope.",
		"2. Make the smallest safe change.",
		"3. Explain the expected effect and any follow-up checks.",
	}, "\n")
}
