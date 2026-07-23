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
	domourmodel "github.com/qtopie/domour/ark/model"
)

func (s *AssistantService) Chat(ctx context.Context, req shared.MotorChatRequest, provider, model string, yield func(event shared.MotorStreamEvent) error) error {
	sessionID := req.SessionID
	var chunkSeq int32
	yieldWithSeq := func(event shared.MotorStreamEvent) error {
		chunkSeq++
		event.ChunkSeq = chunkSeq
		event.MaxSeqChecksum = chunkSeq
		return yield(event)
	}

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
		userMessage = "Hello from Domour Copilot! How can I assist you today?"
	}
	_ = s.AppendHistory(ctx, sessionID, "user", userMessage)

	// Spin up fast OCR-style interception pass in background
	bgCtx := context.WithoutCancel(ctx)
	go trySendChatInterception(bgCtx, s.interceptor, req)

	var toolMgr *tool.Manager
	if s.engine != nil && s.engine.Executor() != nil {
		toolMgr = s.engine.Executor().ToolManager()
	}

	// Build chat prompt with optional editor context (pinned files)
	chatPrompt := BuildChatPrompt(userMessage, req.Workspace, "", "", "")
	if ec := BuildEditorContextPrompt(req.EditorContext); ec != "" {
		chatPrompt = ec + "\n\n" + chatPrompt
	}

	// Deep Think Mode: pure reasoning without tool calling or workflow
	if rmeta.Mode == "deep_think" {
		messages := []*schema.Message{
			schema.SystemMessage(BuildChatSystemPrompt(ctx, toolMgr, userMessage, nil, req.SystemPromptOverride)),
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
		if err := yieldWithSeq(shared.MotorStreamEvent{
			Stage:   "reply",
			Content: content,
			Done:    false,
			Meta:    map[string]string{"provider": resp.Provider, "model": resp.Model},
		}); err != nil {
			return err
		}
		if err := yieldWithSeq(shared.MotorStreamEvent{
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
			schema.SystemMessage(BuildChatSystemPrompt(ctx, toolMgr, userMessage, snapshot.Interception, req.SystemPromptOverride)),
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
			_ = yieldWithSeq(event)
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
	if err := yieldWithSeq(shared.MotorStreamEvent{
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
