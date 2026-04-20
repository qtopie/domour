package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/pkg/brain/diencephalon"
	brainmvp "github.com/qtopie/domour/internal/pkg/brain/mvp"
)

type localBrainClient struct {
	chatModel      diencephalon.Client
	copilotModel   diencephalon.Client
	autopilotModel diencephalon.Client
	fallback       *brainmvp.DiagramBrain
}

func newLocalBrainClient() (BrainClient, error) {
	chatModel, err := diencephalon.NewForEntry(context.Background(), "chat")
	if err != nil {
		return nil, fmt.Errorf("init chat model: %w", err)
	}
	copilotModel, err := diencephalon.NewForEntry(context.Background(), "copilot")
	if err != nil {
		return nil, fmt.Errorf("init copilot model: %w", err)
	}
	autopilotModel, err := diencephalon.NewForEntry(context.Background(), "autopilot")
	if err != nil {
		return nil, fmt.Errorf("init autopilot model: %w", err)
	}

	return &localBrainClient{
		chatModel:      chatModel,
		copilotModel:   copilotModel,
		autopilotModel: autopilotModel,
		fallback:       brainmvp.NewDiagramBrain(),
	}, nil
}

func (b *localBrainClient) StreamChat(ctx context.Context, req BrainChatRequest) (<-chan BrainStreamEvent, error) {
	stream := make(chan BrainStreamEvent, 2)

	go func() {
		defer close(stream)

		if isDiagramLike(req.Message, req.Filename) {
			plan, err := b.PlanDiagram(ctx, BrainDiagramRequest{
				Workspace:   req.Workspace,
				Message:     req.Message,
				Filename:    req.Filename,
				FrontPart:   req.FrontPart,
				BackPart:    req.BackPart,
				Attachments: req.Attachments,
				History:     req.History,
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

		reply, err := b.ChatReply(ctx, req)
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

func (b *localBrainClient) StreamAutopilot(ctx context.Context, req BrainAutopilotRequest) (<-chan BrainStreamEvent, error) {
	stream := make(chan BrainStreamEvent, 4)

	go func() {
		defer close(stream)

		reply, err := b.Autopilot(ctx, req)
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

func (b *localBrainClient) StreamCopilot(ctx context.Context, req BrainCopilotRequest) (<-chan BrainStreamEvent, error) {
	stream := make(chan BrainStreamEvent, 4)

	go func() {
		defer close(stream)

		reply, err := b.Copilot(ctx, req)
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

func (b *localBrainClient) ChatReply(ctx context.Context, req BrainChatRequest) (BrainTextResponse, error) {
	for attempt := 0; attempt < maxChatContextRefreshRounds; attempt++ {
		snapshot := latestChatInterception(req.SessionID, req.Seq, req.Interception)
		messages := []*schema.Message{
			schema.SystemMessage(buildChatSystemPrompt(req.Message, req.Attachments, snapshot.Interception)),
		}
		messages = append(messages, historyToSchema(req.History)...)
		userMessage, err := buildUserInputMessage(
			applyChatInterceptionContext(buildChatPrompt(req.Message, req.Workspace, req.Filename, req.FrontPart, req.BackPart), snapshot.Interception),
			req.Attachments,
		)
		if err != nil {
			return BrainTextResponse{}, err
		}
		messages = append(messages, userMessage)

		reply, err := b.chatModel.GenerateText(ctx, messages)
		if err != nil {
			return BrainTextResponse{}, err
		}

		latest := latestChatInterception(req.SessionID, req.Seq, snapshot.Interception)
		if latest.SemanticVersion > snapshot.SemanticVersion && attempt+1 < maxChatContextRefreshRounds {
			continue
		}

		return BrainTextResponse{
			Content:  reply.Content,
			Provider: reply.Provider,
			Model:    reply.Model,
		}, nil
	}

	return BrainTextResponse{}, fmt.Errorf("chat reply exceeded maximum context refresh rounds")
}

func (b *localBrainClient) PlanDiagram(ctx context.Context, req BrainDiagramRequest) (BrainDiagramResponse, error) {
	format := inferRequestedFormat(req.Message)
	title := inferDiagramTitle(req.Message)

	messages := []*schema.Message{
		schema.SystemMessage("You are Domour Brain. Convert the user's request into valid D2 source only. Do not wrap the result in markdown fences. Keep the diagram concise and directly usable by the d2 CLI."),
	}
	messages = append(messages, historyToSchema(req.History)...)
	userMessage, err := buildUserInputMessage(buildDiagramPrompt(req.Message, req.Workspace, req.Filename, req.FrontPart, req.BackPart, format), req.Attachments)
	if err != nil {
		return BrainDiagramResponse{}, err
	}
	messages = append(messages, userMessage)

	reply, err := b.chatModel.GenerateText(ctx, messages)
	if err == nil {
		d2Source := stripCodeFence(reply.Content)
		return BrainDiagramResponse{
			Summary:  fmt.Sprintf("Brain used %s to generate a D2 diagram and selected %s rendering.", reply.Provider, format),
			Route:    "render_d2",
			Format:   format,
			Title:    title,
			Diagram:  d2Source,
			Provider: reply.Provider,
			Model:    reply.Model,
		}, nil
	}

	fallback, fallbackErr := b.fallback.Think(ctx, req.Message)
	if fallbackErr != nil {
		return BrainDiagramResponse{}, err
	}
	return BrainDiagramResponse{
		Summary:  fallback.Summary,
		Route:    fallback.Route,
		Format:   fallback.Format,
		Title:    fallback.Title,
		Diagram:  fallback.Diagram,
		Provider: "mvp-rule-brain",
	}, nil
}

func (b *localBrainClient) Copilot(ctx context.Context, req BrainCopilotRequest) (BrainTextResponse, error) {
	messages := []*schema.Message{
		schema.SystemMessage("You are Domour Copilot. Produce the smallest correct patch or code suggestion for the user's request. Prefer concrete code over high-level advice when enough context is present."),
	}
	messages = append(messages, historyToSchema(req.History)...)
	userMessage, err := buildUserInputMessage(buildCopilotPrompt(req.Message, req.Workspace, req.Filename, req.CodeBefore, req.CodeAfter, req.CursorOffset), req.Attachments)
	if err != nil {
		return BrainTextResponse{}, err
	}
	messages = append(messages, userMessage)

	reply, err := b.copilotModel.GenerateText(ctx, messages)
	if err == nil {
		return BrainTextResponse{
			Content:  reply.Content,
			Provider: reply.Provider,
			Model:    reply.Model,
		}, nil
	}
	return BrainTextResponse{
		Content:  buildFallbackCopilotReply(req),
		Provider: "mvp-rule-brain",
	}, nil
}

func (b *localBrainClient) Autopilot(ctx context.Context, req BrainAutopilotRequest) (BrainTextResponse, error) {
	messages := []*schema.Message{
		schema.SystemMessage("You are Domour Autopilot. Produce a concise execution plan tailored to the user's goal and constraints. Prefer numbered steps."),
	}
	userMessage, err := buildUserInputMessage(buildAutopilotPrompt(req.Goal, req.Workspace, req.Constraints, req.MaxSteps), req.Attachments)
	if err != nil {
		return BrainTextResponse{}, err
	}
	messages = append(messages, userMessage)
	messages = append(historyToSchema(req.History), messages...)

	reply, err := b.autopilotModel.GenerateText(ctx, messages)
	if err == nil {
		return BrainTextResponse{
			Content:  reply.Content,
			Provider: reply.Provider,
			Model:    reply.Model,
		}, nil
	}
	return BrainTextResponse{
		Content:  buildFallbackAutopilotPlan(req),
		Provider: "mvp-rule-brain",
	}, nil
}

func splitReplyChunks(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	parts := strings.Split(content, "\n\n")
	chunks := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		chunks = append(chunks, part)
	}
	if len(chunks) == 0 {
		return []string{content}
	}
	return chunks
}

func buildFallbackAutopilotPlan(req BrainAutopilotRequest) string {
	parts := []string{
		fmt.Sprintf("Goal: %s", firstNonEmpty(strings.TrimSpace(req.Goal), "Clarify the goal.")),
		fmt.Sprintf("Workspace: %s", firstNonEmpty(strings.TrimSpace(req.Workspace), "not provided")),
	}
	if len(req.Constraints) > 0 {
		parts = append(parts, "Constraints: "+strings.Join(req.Constraints, "; "))
	} else {
		parts = append(parts, "Constraints: none provided")
	}

	plan := []string{
		"1. Clarify the target outputs, streaming boundaries, and refusal rules.",
		"2. Identify the agent, brain, and motor responsibilities plus their exchange events.",
		"3. Implement the smallest safe orchestration path first and keep the final output gate in motor.",
		"4. Add validation, fallback behavior, and explicit safety refusal handling.",
	}
	if req.MaxSteps > 0 && int(req.MaxSteps) < len(plan) {
		plan = plan[:req.MaxSteps]
	}
	return strings.Join(append(parts, plan...), "\n")
}

func buildFallbackCopilotReply(req BrainCopilotRequest) string {
	parts := []string{
		fmt.Sprintf("Task: %s", firstNonEmpty(strings.TrimSpace(req.Message), "Clarify the requested code change.")),
		fmt.Sprintf("Workspace: %s", firstNonEmpty(strings.TrimSpace(req.Workspace), "not provided")),
	}
	if file := strings.TrimSpace(req.Filename); file != "" {
		parts = append(parts, "Target file: "+file)
	}
	if strings.TrimSpace(req.CodeBefore) != "" || strings.TrimSpace(req.CodeAfter) != "" {
		parts = append(parts, "Context detected around the cursor. Prefer a minimal local edit instead of broad refactoring.")
	}

	steps := []string{
		"1. Identify the smallest symbol or block that needs to change.",
		"2. Update names or logic consistently at the declaration and call sites.",
		"3. Keep surrounding behavior stable unless the request explicitly asks for more.",
		"4. Re-run the nearest build or test path that covers the edited file.",
	}
	return strings.Join(append(parts, steps...), "\n")
}
