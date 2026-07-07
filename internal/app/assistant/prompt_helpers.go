package assistant

import (
	"context"
	"fmt"
	"strings"

	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/bionic/tool"
	bioniccontext "github.com/qtopie/domour/internal/bionic/context"
	appconfig "github.com/qtopie/domour/internal/config"
	"github.com/qtopie/domour/internal/infra/llm"
	providerruntime "github.com/qtopie/domour/internal/infra/llm/runtime"
)

func BuildChatPrompt(userMessage, workspace, filename, frontPart, backPart string) string {
	parts := []string{fmt.Sprintf("User request:\n%s", userMessage)}
	if shared.WantsOCRTask(userMessage) {
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

// BuildEditorContextPrompt builds a prompt section from pinned editor context files.
func BuildEditorContextPrompt(ec *shared.EditorContext) string {
	if ec == nil || len(ec.PinnedFiles) == 0 {
		return ""
	}
	var parts []string
	parts = append(parts, "The user has pinned the following files for context:")
	for _, f := range ec.PinnedFiles {
		lang := f.Language
		if lang == "" {
			lang = guessLanguage(f.Path)
		}
		parts = append(parts, fmt.Sprintf("--- %s ---\n```%s\n%s\n```", f.Path, lang, f.Content))
	}
	return strings.Join(parts, "\n\n")
}

func guessLanguage(path string) string {
	dot := strings.LastIndex(path, ".")
	if dot < 0 {
		return ""
	}
	ext := strings.ToLower(path[dot+1:])
	switch ext {
	case "go":
		return "go"
	case "py":
		return "python"
	case "js", "ts", "jsx", "tsx":
		return ext
	case "rs":
		return "rust"
	case "java":
		return "java"
	case "c", "h":
		return "c"
	case "cpp", "cc", "cxx", "hpp":
		return "cpp"
	case "rb":
		return "ruby"
	case "json":
		return "json"
	case "yaml", "yml":
		return "yaml"
	case "md", "markdown":
		return "markdown"
	case "sql":
		return "sql"
	case "sh", "bash":
		return "bash"
	case "dockerfile":
		return "dockerfile"
	case "html", "css", "scss":
		return ext
	case "toml":
		return "toml"
	case "proto":
		return "protobuf"
	default:
		return ""
	}
}

func BuildChatSystemPrompt(ctx context.Context, toolMgr *tool.Manager, message string, attachments []shared.BrainAttachment, interception *shared.ChatInterception) string {
	prompt := "You are Domour Chat. Reply clearly and directly to the user. Use the provided workspace context when useful."
	if HasImageAttachments(attachments) {
		prompt += " When image attachments are present and the user asks for OCR, text extraction, transcription, or document reading, extract the visible text faithfully and preserve the original structure when possible."
	}
	prompt += bioniccontext.BuildInterceptionSystemNote(interception)
	if shared.WantsOCRTask(message) {
		prompt += " This request is OCR-focused: prioritize accurate text extraction over summary, keep reading order, and mark uncertain characters as [unclear]."
	}

	if toolMgr != nil {
		if matched := toolMgr.DetectActiveSkill(ctx, message); matched != "" {
			if activePrompt, err := toolMgr.BuildActiveSkillPrompt(ctx, matched); err == nil && activePrompt != "" {
				prompt += "\n\n" + activePrompt
			}
		} else {
			if availablePrompt, err := toolMgr.BuildAvailableSkillsPrompt(ctx); err == nil && availablePrompt != "" {
				prompt += "\n\n" + availablePrompt
			}
		}
	}

	// Active when in diagnostic mode, placed at the very end.
	rmeta := providerruntime.RequestMetadataFromContext(ctx)
	if rmeta.Mode == "diagnostic" && toolMgr != nil {
		if activePrompt, err := toolMgr.BuildActiveSkillPrompt(ctx, "cosmos-star:diagnostic"); err == nil && activePrompt != "" {
			prompt += "\n\n" + activePrompt
		}
	}

	return prompt
}

func BuildDiagramPrompt(userMessage, workspace, filename, frontPart, backPart, format string) string {
	return strings.Join([]string{
		fmt.Sprintf("Render format: %s", format),
		BuildChatPrompt(userMessage, workspace, filename, frontPart, backPart),
		"Return D2 source only.",
	}, "\n\n")
}

func IsDiagramLike(message, filename string) bool {
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{message, filename}, " ")))
	for _, marker := range []string{"架构图", "流程图", "时序图", "拓扑图", "diagram", "architecture", "flowchart", "sequence", "d2", "svg", "html", "web page", "网页"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func InferRequestedFormat(message string) string {
	text := strings.ToLower(message)
	switch {
	case strings.Contains(text, "网页"), strings.Contains(text, "html"), strings.Contains(text, "web"):
		return "html"
	default:
		return "svg"
	}
}

func InferDiagramTitle(message string) string {
	title := strings.TrimSpace(strings.ReplaceAll(message, "\n", " "))
	if title == "" {
		return "System Architecture"
	}
	if len(title) > 48 {
		return title[:48]
	}
	return title
}

func HasImageAttachments(attachments []shared.BrainAttachment) bool {
	for _, attachment := range attachments {
		if strings.HasPrefix(llm.NormalizeAttachmentMIMEType(attachment), "image/") {
			return true
		}
	}
	return false
}

func BuildCopilotPrompt(userMessage, workspace, filename, before, after string, cursorOffset int32) string {
	parts := []string{strings.TrimSpace(userMessage)}
	if workspace := strings.TrimSpace(workspace); workspace != "" {
		parts = append(parts, "Workspace: "+workspace)
	}
	if filename := strings.TrimSpace(filename); filename != "" {
		parts = append(parts, "Target file: "+filename)
	}
	if before := strings.TrimSpace(before); before != "" {
		parts = append(parts, "Code before cursor:\n"+before)
	}
	if after := strings.TrimSpace(after); after != "" {
		parts = append(parts, "Code after cursor:\n"+after)
	}
	if cursorOffset > 0 {
		parts = append(parts, fmt.Sprintf("Cursor offset: %d", cursorOffset))
	}
	return strings.Join(parts, "\n\n")
}

func BuildAutopilotPrompt(goal, workspace string, constraints []string, maxSteps int32) string {
	parts := []string{
		"Goal: " + goal,
		"Workspace: " + FirstNonEmpty(strings.TrimSpace(workspace), "not provided"),
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

func FirstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func BuildChatSummaryMessage(message, workspace, filename string, historyCount int, summary string) string {
	parts := []string{
		"Domour MVP chat is online.",
		fmt.Sprintf("Message: %s", FirstNonEmpty(strings.TrimSpace(message), "Hello.")),
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

func BuildRenderedReply(d2Source, rendered string) string {
	return strings.Join([]string{
		"Brain produced the following D2 diagram:",
		"```d2",
		d2Source,
		"```",
		"Motor rendered the artifact below:",
		rendered,
	}, "\n")
}

func EstimateTokenCount(content string) int {
	var total float64
	for _, r := range content {
		if r >= 0x4e00 && r <= 0x9fff {
			total += 0.8
		} else if r == '\n' || r == '\t' || r == ' ' {
			total += 0.5
		} else {
			total += 0.3
		}
	}
	return int(total)
}

func GetModelThresholds(cfg appconfig.DomourConfig, provider, model string) (int, int) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.ToLower(strings.TrimSpace(model))

	if cfg.Providers != nil {
		pKey := provider
		if provider == "copilot-cli" || provider == "github-copilot" {
			pKey = "github-copilot-cli"
		}
		if pCfg, ok := cfg.Providers[pKey]; ok {
			if pCfg.MaxActiveTokens > 0 && pCfg.CompressTriggerTokens > 0 {
				return pCfg.MaxActiveTokens, pCfg.CompressTriggerTokens
			}
		}
	}

	if cfg.MaxActiveTokens > 0 && cfg.CompressTriggerTokens > 0 {
		return cfg.MaxActiveTokens, cfg.CompressTriggerTokens
	}

	if strings.Contains(provider, "gemini") || strings.Contains(model, "gemini") {
		return 64000, 32000
	}
	if strings.Contains(provider, "openai") || strings.Contains(model, "gpt-") ||
		strings.Contains(provider, "deepseek") || strings.Contains(model, "deepseek") ||
		strings.Contains(provider, "qwen") || strings.Contains(model, "qwen") ||
		strings.Contains(provider, "agy-sdk") || strings.Contains(provider, "agy_sdk") {
		return 24000, 16000
	}
	if strings.Contains(provider, "ollama") || strings.Contains(model, "ollama") {
		return 4000, 3000
	}
	if strings.Contains(provider, "copilot") || strings.Contains(provider, "qoder") {
		return 3000, 2000
	}

	return 16000, 8000
}
