package agent

import (
	"context"
	"fmt"
	"strings"

	chatpb "github.com/qtopie/domour/gen/assistant/chat"
	"google.golang.org/grpc"
)

func (s *Server) Chat(req *chatpb.ChatRequest, stream grpc.ServerStreamingServer[chatpb.ChatResponse]) error {
	sessionID := normalizeSessionID(req.GetSessionId())
	s.logCall(stream.Context(), "Chat", sessionID)

	ctx := withRuntimeMetadata(stream.Context(), sessionID, req.GetWorkspace())
	history, _ := s.getHistory(ctx, sessionID)

	// Check provider readiness
	brainClient, err := s.brain.GetClient(ctx, "chat")
	if err == nil {
		if ready, readyErr := brainClient.IsReady(ctx); !ready || readyErr != nil {
			err = readyErr
			if err == nil {
				err = fmt.Errorf("provider %s is not ready", brainClient.Provider())
			}
			s.logError(stream.Context(), "Chat.Readiness", sessionID, err)
			return err
		}
	}

	userMessage := strings.TrimSpace(req.GetMessage())
	if userMessage == "" {
		userMessage = "Hello from Domour MVP."
	}
	_ = s.appendHistory(ctx, sessionID, "user", userMessage)

	brainCtx, brainCancel := context.WithCancel(ctx)
	defer brainCancel()

	bridge := newSessionBridge()
	brainReq := BrainChatRequest{
		SessionID:   sessionID,
		Seq:         req.GetSeq(),
		Workspace:   req.GetWorkspace(),
		Message:     userMessage,
		Filename:    req.GetFilename(),
		FrontPart:   req.GetFrontPart(),
		BackPart:    req.GetBackPart(),
		Attachments: attachmentsFromProto(req.GetAttachments()),
		History:     history,
	}
	motorReq := MotorChatRequest{
		SessionID:   sessionID,
		Seq:         req.GetSeq(),
		Workspace:   req.GetWorkspace(),
		Message:     userMessage,
		Filename:    req.GetFilename(),
		FrontPart:   req.GetFrontPart(),
		BackPart:    req.GetBackPart(),
		Attachments: attachmentsFromProto(req.GetAttachments()),
		History:     history,
	}

	go s.streamBrainToBridge(brainCtx, brainCancel, brainReq, bridge)
	go s.streamMotorToBridge(ctx, motorReq, bridge)

	var replyParts []string
	for event := range bridge.MotorOut {
		if event.Err != nil {
			s.logError(stream.Context(), "Chat", sessionID, event.Err)
			return event.Err
		}
		if strings.TrimSpace(event.Content) != "" {
			replyParts = append(replyParts, event.Content)
		}

		if err := stream.Send(&chatpb.ChatResponse{
			SessionId: sessionID,
			Seq:       req.GetSeq(),
			Content:   event.Content,
			Done:      event.Done,
			Meta:      mergeChatMeta(event.Meta, event.Stage),
		}); err != nil {
			s.logError(stream.Context(), "Chat", sessionID, err)
			return err
		}

		if event.Done {
			_ = s.appendHistory(ctx, sessionID, "assistant", strings.Join(replyParts, "\n"))
		}
	}
	return nil
}

func buildChatPrompt(userMessage, workspace, filename, frontPart, backPart string) string {
	parts := []string{fmt.Sprintf("User request:\n%s", userMessage)}
	if wantsOCRTask(userMessage) {
		parts = append(parts,
			"Task mode: OCR",
			"OCR requirements:\n- Extract visible text faithfully.\n- Preserve natural reading order and line breaks when possible.\n- Keep tables, forms, or lists structured instead of summarizing them.\n- If some characters are unclear, mark them as [unclear].\n- Do not translate or summarize unless the user explicitly asks.",
		)
	}
	if workspace := strings.TrimSpace(workspace); workspace != "" {
		parts = append(parts, fmt.Sprintf("Workspace: %s", workspace))
	}
	if filename := strings.TrimSpace(filename); filename != "" {
		parts = append(parts, fmt.Sprintf("Current file: %s", filename))
	}
	if front := strings.TrimSpace(frontPart); front != "" {
		parts = append(parts, "Code before cursor:\n"+front)
	}
	if back := strings.TrimSpace(backPart); back != "" {
		parts = append(parts, "Code after cursor:\n"+back)
	}
	return strings.Join(parts, "\n\n")
}

func buildChatSystemPrompt(message string, attachments []BrainAttachment, interception *ChatInterception) string {
	prompt := "You are Domour Chat. Reply clearly and directly to the user. Use the provided workspace context when useful."
	if hasImageAttachments(attachments) {
		prompt += " When image attachments are present and the user asks for OCR, text extraction, transcription, or document reading, extract the visible text faithfully and preserve the original structure when possible."
	}
	prompt += buildInterceptionSystemNote(interception)
	if wantsOCRTask(message) {
		prompt += " This request is OCR-focused: prioritize accurate text extraction over summary, keep reading order, and mark uncertain characters as [unclear]."
	}
	return prompt
}

func buildDiagramPrompt(userMessage, workspace, filename, frontPart, backPart, format string) string {
	return strings.Join([]string{
		fmt.Sprintf("Render format: %s", format),
		buildChatPrompt(userMessage, workspace, filename, frontPart, backPart),
		"Return D2 source only.",
	}, "\n\n")
}

func isDiagramLike(message, filename string) bool {
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{message, filename}, " ")))
	for _, marker := range []string{"架构图", "流程图", "时序图", "拓扑图", "diagram", "architecture", "flowchart", "sequence", "d2", "svg", "html", "web page", "网页"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
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

func buildChatSummaryMessage(message, workspace, filename string, historyCount int, summary string) string {
	parts := []string{
		"Domour MVP chat is online.",
		fmt.Sprintf("Message: %s", firstNonEmpty(strings.TrimSpace(message), "Hello.")),
	}
	if workspace := strings.TrimSpace(workspace); workspace != "" {
		parts = append(parts, fmt.Sprintf("Workspace: %s", workspace))
	}
	if filename := strings.TrimSpace(filename); filename != "" {
		parts = append(parts, fmt.Sprintf("File: %s", filename))
	}
	parts = append(parts,
		fmt.Sprintf("History messages: %d", historyCount),
		summary,
	)
	return strings.Join(parts, "\n")
}

func mergeChatMeta(meta map[string]string, stage string) map[string]string {
	out := map[string]string{
		"entry": "chat",
		"mode":  "mvp",
		"stage": stage,
	}
	for k, v := range meta {
		out[k] = v
	}
	return out
}

func buildRenderedReply(d2Source, rendered string) string {
	return strings.Join([]string{
		"Brain produced the following D2 diagram:",
		"```d2",
		d2Source,
		"```",
		"Motor rendered the artifact below:",
		rendered,
	}, "\n")
}

func wantsOCRTask(message string) bool {
	text := strings.ToLower(strings.TrimSpace(message))
	if text == "" {
		return false
	}
	for _, marker := range []string{
		"ocr", "extract text", "text extraction", "transcribe", "read the image", "scan text",
		"识别文字", "识别图片中的文字", "提取文字", "提取文本", "图片文字", "图中文字", "文字识别", "ocr识别", "转文字", "读图",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func hasImageAttachments(attachments []BrainAttachment) bool {
	for _, attachment := range attachments {
		if strings.HasPrefix(normalizeAttachmentMIMEType(attachment), "image/") {
			return true
		}
	}
	return false
}
