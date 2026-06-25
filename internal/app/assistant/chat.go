package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	bioniccontext "github.com/qtopie/domour/internal/bionic/context"
	"github.com/qtopie/domour/internal/bionic/tool"
	"github.com/qtopie/domour/internal/cognitor/proxy"
	appconfig "github.com/qtopie/domour/internal/config"
	"github.com/qtopie/domour/internal/infra/dapr"
	"github.com/qtopie/domour/internal/infra/llm"
	providerruntime "github.com/qtopie/domour/internal/infra/llm/runtime"
	domourmodel "github.com/qtopie/domour/pkg/model"
)

func (s *AssistantService) Chat(ctx context.Context, req shared.MotorChatRequest, provider, model string, yield func(event shared.MotorStreamEvent) error) error {
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

	// Resolve Brain LLM Client via System Mode Model Selection
	rmeta := providerruntime.RequestMetadataFromContext(ctx)

	// Mode→Model registration lookup: if provider or model is still empty
	// (not overridden by caller or session), query the model registry using
	// the current system mode's tag rules.
	if provider == "" || model == "" {
		cfg, cfgErr := appconfig.LoadDomourConfig()
		if cfgErr == nil {
			mm := cfg.ModeMapping(rmeta.Mode)
			if p, m, qErr := domourmodel.DefaultRegistry().QueryBestModel(mm.Require, mm.Prefer, mm.Exclude); qErr == nil && p != "" {
				if provider == "" {
					provider = p
				}
				if model == "" {
					model = m
				}
			}
		}
	}

	brainClient, err := s.engine.Cognitor().GetClientWithOverride(ctx, "chat", provider, model)
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
		userMessage = "Hello from Domour MVP."
	}
	_ = s.AppendHistory(ctx, sessionID, "user", userMessage)

	// Spin up fast OCR-style interception pass in background
	bgCtx := context.WithoutCancel(ctx)
	go trySendChatInterception(bgCtx, s.interceptor, req)

	// Build chat prompt with optional editor context (pinned files)
	chatPrompt := BuildChatPrompt(userMessage, req.Workspace, "", "", "")
	if ec := BuildEditorContextPrompt(req.EditorContext); ec != "" {
		chatPrompt = ec + "\n\n" + chatPrompt
	}

	// Check if this looks like a diagram rendering request
	if IsDiagramLike(userMessage, "") {
		format := InferRequestedFormat(userMessage)
		title := InferDiagramTitle(userMessage)

		messages := []*schema.Message{
			schema.SystemMessage("You are Domour Brain. Convert the user's request into valid D2 source only. Do not wrap the result in markdown fences. Keep the diagram concise and directly usable by the d2 CLI."),
		}
		messages = append(messages, llm.HistoryToSchema(sess.History, sess.MemorySummary)...)
		userMsg, err := llm.BuildUserInputMessage(
			BuildDiagramPrompt(userMessage, req.Workspace, "", "", "", format),
			req.Attachments,
		)
		if err != nil {
			return fmt.Errorf("build diagram prompt: %w", err)
		}
		messages = append(messages, userMsg)

		resp, err := brainClient.GenerateText(ctx, messages)
		var diagram string
		var summary string
		var providerName, modelName string
		if err != nil {
			fallback := &diagramFallbackBrain{}
			fbResp, fbErr := fallback.Think(ctx, userMessage)
			if fbErr != nil {
				return fmt.Errorf("diagram fallback think: %w", fbErr)
			}
			diagram = fbResp.Diagram
			summary = fbResp.Summary
			providerName = fbResp.Provider
			modelName = fbResp.Model
		} else {
			diagram = llm.StripCodeFence(resp.Content)
			summary = fmt.Sprintf("Brain used %s to generate a D2 diagram and selected %s rendering.", resp.Provider, format)
			providerName = resp.Provider
			modelName = resp.Model
		}

		// Safety Interception (Veto)
		if s.engine.Executor().Veto(ctx, userMessage) || s.engine.Executor().Veto(ctx, diagram) {
			return yieldRefusal(yield, providerName, modelName)
		}

		// Send summary to client
		summaryContent := BuildChatSummaryMessage(userMessage, req.Workspace, "", len(sess.History), summary)
		if err := yield(shared.MotorStreamEvent{
			Stage:   "brain",
			Content: summaryContent,
			Done:    false,
			Meta:    map[string]string{"provider": providerName, "model": modelName, "format": format},
		}); err != nil {
			return err
		}

		// Execute physical render command via Motor
		result, err := s.engine.Executor().Execute(ctx, tool.Command{
			ID:     fmt.Sprintf("chat-%d-render", req.Seq),
			Action: "render_d2",
			Input: map[string]interface{}{
				"source": diagram,
				"format": format,
				"title":  title,
			},
		})
		if err != nil {
			return fmt.Errorf("motor execute render: %w", err)
		}

		renderedContent := BuildRenderedReply(diagram, result.Observation)
		if err := yield(shared.MotorStreamEvent{
			Stage:   "motor",
			Content: renderedContent,
			Done:    true,
			Meta:    map[string]string{"provider": providerName, "model": modelName, "format": result.Meta["format"]},
		}); err != nil {
			return err
		}

		_ = s.AppendHistoryWithMeta(ctx, sessionID, "assistant", renderedContent, providerName, modelName)
		return nil
	}

	// Deep Think Mode: pure reasoning without tool calling or workflow
	if rmeta.Mode == "deep_think" {
		messages := []*schema.Message{
			schema.SystemMessage(BuildChatSystemPrompt(userMessage, req.Attachments, nil)),
		}
		messages = append(messages, llm.HistoryToSchema(sess.History, sess.MemorySummary)...)
		userMsg, err := llm.BuildUserInputMessage(
			chatPrompt,
			req.Attachments,
		)
		if err != nil {
			return fmt.Errorf("build deep think prompt: %w", err)
		}
		messages = append(messages, userMsg)

		resp, err := brainClient.GenerateText(ctx, messages)
		if err != nil {
			return fmt.Errorf("deep think generate: %w", err)
		}

		content := strings.TrimSpace(resp.Content)
		if content == "" {
			return fmt.Errorf("deep think: empty response")
		}

		// Stream content
		if err := yield(shared.MotorStreamEvent{
			Stage:   "reply",
			Content: content,
			Done:    false,
			Meta:    map[string]string{"provider": resp.Provider, "model": resp.Model},
		}); err != nil {
			return err
		}
		if err := yield(shared.MotorStreamEvent{
			Stage:   "reply",
			Content: "",
			Done:    true,
			Meta:    map[string]string{"provider": resp.Provider, "model": resp.Model},
		}); err != nil {
			return err
		}

		_ = s.AppendHistoryWithMeta(ctx, sessionID, "assistant", content, resp.Provider, resp.Model)
		return nil
	}

	// Normal Chat Generation
	var finalContent string
	var lastProvider, lastModel string

	for attempt := 0; attempt < bioniccontext.MaxChatContextRefreshRounds; attempt++ {
		snapshot := bioniccontext.LatestChatInterception(sessionID, req.Seq, nil)
		messages := []*schema.Message{
			schema.SystemMessage(BuildChatSystemPrompt(userMessage, req.Attachments, snapshot.Interception)),
		}
		messages = append(messages, llm.HistoryToSchema(sess.History, sess.MemorySummary)...)
		userMsg, err := llm.BuildUserInputMessage(
			bioniccontext.ApplyChatInterceptionContext(chatPrompt, snapshot.Interception),
			req.Attachments,
		)
		if err != nil {
			return fmt.Errorf("build chat prompt: %w", err)
		}
		messages = append(messages, userMsg)

		workflowID := fmt.Sprintf("chat-%s-%d-attempt-%d", sessionID, req.Seq, attempt)
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
			Stage:       "reply",
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

		// Re-evaluate context refresh loop
		latest := bioniccontext.LatestChatInterception(sessionID, req.Seq, snapshot.Interception)
		if latest.SemanticVersion > snapshot.SemanticVersion && attempt+1 < bioniccontext.MaxChatContextRefreshRounds {
			continue
		}

		finalContent = wfStatus.Result.Content
		lastProvider = brainClient.Provider()
		lastModel = brainClient.Model()
		break
	}

	// Safety check / Veto
	if s.engine.Executor().Veto(ctx, userMessage) || s.engine.Executor().Veto(ctx, finalContent) {
		return yieldRefusal(yield, lastProvider, lastModel)
	}

	// Stream final done message
	if err := yield(shared.MotorStreamEvent{
		Stage:   "reply",
		Content: "",
		Done:    true,
		Meta:    map[string]string{"provider": lastProvider, "model": lastModel},
	}); err != nil {
		return err
	}

	_ = s.AppendHistoryWithMeta(ctx, sessionID, "assistant", finalContent, lastProvider, lastModel)
	return nil
}

func yieldRefusal(yield func(event shared.MotorStreamEvent) error, provider, model string) error {
	refusal := "Motor refused to execute or return this result because it appears unsafe."
	return yield(shared.MotorStreamEvent{
		Stage:   "motor",
		Content: refusal,
		Done:    true,
		Meta: map[string]string{
			"provider": provider,
			"model":    model,
			"policy":   "safety",
		},
	})
}

func trySendChatInterception(ctx context.Context, interceptor bioniccontext.ChatContextInterceptor, req shared.MotorChatRequest) {
	if interceptor == nil || len(imageOnlyAttachments(req.Attachments)) == 0 {
		return
	}
	interception, err := interceptor.InterceptChatContext(ctx, req)
	if err != nil || interception == nil {
		return
	}
	bioniccontext.DefaultChatContextWorkingSet.Update(req.SessionID, req.Seq, interception)
}

func imageOnlyAttachments(attachments []shared.BrainAttachment) []shared.BrainAttachment {
	if len(attachments) == 0 {
		return nil
	}
	filtered := make([]shared.BrainAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		if strings.HasPrefix(llm.NormalizeAttachmentMIMEType(attachment), "image/") {
			filtered = append(filtered, attachment)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func splitReplyChunks(content string) []string {
	runes := []rune(content)
	if len(runes) == 0 {
		return []string{""}
	}
	var chunks []string
	chunkSize := 8
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}

type diagramFallbackBrain struct{}

func (b *diagramFallbackBrain) Think(ctx context.Context, prompt string) (shared.BrainDiagramResponse, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "Draw a system architecture diagram"
	}

	format := inferRequestedFormat(prompt)
	title := inferDiagramTitle(prompt)
	diagram := buildFallbackD2Diagram(title, prompt, format)

	return shared.BrainDiagramResponse{
		Summary:  fmt.Sprintf("Brain inferred a diagram request and chose %s rendering.", format),
		Route:    "render_d2",
		Format:   format,
		Title:    title,
		Diagram:  diagram,
		Provider: "mvp-rule-brain",
	}, nil
}

func buildFallbackD2Diagram(title, prompt, format string) string {
	return fmt.Sprintf(`direction: right

title: %q

user: User
agent: Agent
brain: Brain
motor: Motor
tool: "D2 Render Tool"
artifact: "%s output"

user -> agent: "chat request"
agent -> brain: "reason about request"
brain -> motor: "D2 diagram plan"
motor -> tool: "render diagram"
tool -> agent: "artifact"
agent -> user: "final response"

brain.note: "Prompt: %s"
`, title, strings.ToUpper(format), escapeFallbackLabel(prompt))
}

func escapeFallbackLabel(value string) string {
	value = strings.ReplaceAll(value, `"`, `'`)
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

func inferRequestedFormat(message string) string {
	text := strings.ToLower(message)
	switch {
	case strings.Contains(text, "网页"), strings.Contains(text, "html"), strings.Contains(text, "web"):
		return "html"
	default:
		return "svg"
	}
}

func inferDiagramTitle(message string) string {
	title := strings.TrimSpace(strings.ReplaceAll(message, "\n", " "))
	if title == "" {
		return "System Architecture"
	}
	if len(title) > 48 {
		return title[:48]
	}
	return title
}
