package agent

import (
	"context"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	appconfig "github.com/qtopie/domour/internal/app/config"
	"github.com/qtopie/domour/internal/infra/cache/l1"
	"github.com/qtopie/domour/internal/pkg/brain/diencephalon"
)

var (
	chatOCRContextTimeout       = 2 * time.Second
	maxChatContextRefreshRounds = 3

	defaultChatContextWorkingSet = newChatContextWorkingSet(1024, 15*time.Minute)
)

type chatContextInterceptor interface {
	InterceptChatContext(ctx context.Context, req MotorChatRequest) (*ChatInterception, error)
}

type llmChatContextInterceptor struct{}

type chatContextSnapshot struct {
	Interception    *ChatInterception
	RawVersion      int64
	SemanticVersion int64
}

type chatContextState struct {
	chatContextSnapshot
	rawSignature      string
	semanticSignature string
}

type chatContextWorkingSet struct {
	mu    sync.Mutex
	cache *l1.Cache[string, chatContextState]
	data  map[string]chatContextState
}

func newChatContextInterceptor() chatContextInterceptor {
	return &llmChatContextInterceptor{}
}

func newChatContextWorkingSet(capacity int, ttl time.Duration) *chatContextWorkingSet {
	cache, err := l1.NewCache[string, chatContextState](capacity, ttl)
	if err != nil {
		return &chatContextWorkingSet{data: map[string]chatContextState{}}
	}
	return &chatContextWorkingSet{cache: cache}
}

func (i *llmChatContextInterceptor) InterceptChatContext(ctx context.Context, req MotorChatRequest) (*ChatInterception, error) {
	attachments := imageOnlyAttachments(req.Attachments)
	if len(attachments) == 0 {
		return nil, nil
	}

	client, err := newOCRInterceptionClient(ctx)
	if err != nil || client == nil {
		return nil, err
	}

	ocrCtx, cancel := context.WithTimeout(ctx, chatOCRContextTimeout)
	defer cancel()

	messages := []*schema.Message{
		schema.SystemMessage("You are Domour Motor. Run a fast OCR-style interception pass over the attached image evidence so the main assistant can avoid factual mistakes."),
	}
	userMessage, err := buildUserInputMessage(buildOCRInterceptionPrompt(req.Message), attachments)
	if err != nil {
		return nil, err
	}
	messages = append(messages, userMessage)

	reply, err := client.GenerateText(ocrCtx, messages)
	if err != nil {
		return nil, err
	}

	interception := parseChatInterception(stripCodeFence(reply.Content))
	if interception == nil {
		return nil, nil
	}
	interception.Source = pickFirstNonEmpty(strings.TrimSpace(interception.Source), reply.Provider, "motor-ocr")
	return interception, nil
}

func buildOCRInterceptionPrompt(message string) string {
	parts := []string{
		"The attached image(s) belong to the current user request.",
		"Return concise plain text with exactly these sections:",
		"SUMMARY:",
		"KEY_FACTS:",
		"OCR_TEXT:",
		"In SUMMARY, write one short sentence describing the document or image type.",
		"In KEY_FACTS, list exact high-risk facts visible in the image such as numbers, dates, totals, IDs, names, labels, and short field/value pairs.",
		"In OCR_TEXT, include the most relevant visible text verbatim in natural reading order.",
		"Do not add markdown fences.",
	}
	if wantsOCRTask(message) {
		parts = append(parts, "The user explicitly wants OCR. Be more exhaustive in OCR_TEXT and preserve structure when possible.")
	}
	return strings.Join(parts, "\n")
}

func parseChatInterception(content string) *ChatInterception {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	result := &ChatInterception{}
	current := ""
	for _, rawLine := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		switch strings.TrimSuffix(strings.ToUpper(line), ":") {
		case "SUMMARY":
			current = "summary"
			continue
		case "KEY_FACTS":
			current = "facts"
			continue
		case "OCR_TEXT":
			current = "ocr"
			continue
		}

		switch current {
		case "summary":
			if result.Summary == "" {
				result.Summary = line
			} else {
				result.Summary += " " + line
			}
		case "facts":
			line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
			if line != "" {
				result.KeyFacts = append(result.KeyFacts, line)
			}
		case "ocr":
			if result.OCRText == "" {
				result.OCRText = line
			} else {
				result.OCRText += "\n" + line
			}
		default:
			if result.OCRText == "" {
				result.OCRText = line
			} else {
				result.OCRText += "\n" + line
			}
		}
	}

	result.OCRText = truncateInterceptionText(result.OCRText, 1600)
	result.KeyFacts = normalizeKeyFacts(result.KeyFacts)
	if result.Summary == "" && len(result.KeyFacts) == 0 && result.OCRText == "" {
		return nil
	}
	return result
}

func truncateInterceptionText(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxChars <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	return strings.TrimSpace(string(runes[:maxChars])) + "\n...[truncated]"
}

func imageOnlyAttachments(attachments []BrainAttachment) []BrainAttachment {
	if len(attachments) == 0 {
		return nil
	}
	filtered := make([]BrainAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		if strings.HasPrefix(normalizeAttachmentMIMEType(attachment), "image/") {
			filtered = append(filtered, attachment)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func applyChatInterceptionContext(prompt string, interception *ChatInterception) string {
	if interception == nil {
		return prompt
	}

	parts := []string{strings.TrimSpace(prompt)}
	parts = append(parts,
		"Motor context interception:",
		"- Treat the following OCR-derived evidence as high-priority grounding for image attachments.",
		"- Prefer the OCR evidence over visual guessing when they conflict.",
		"- If the OCR evidence is partial or uncertain, say so explicitly instead of inventing details.",
	)
	if summary := strings.TrimSpace(interception.Summary); summary != "" {
		parts = append(parts, "Interception summary:\n"+summary)
	}
	if len(interception.KeyFacts) > 0 {
		parts = append(parts, "Key facts:\n- "+strings.Join(interception.KeyFacts, "\n- "))
	}
	if ocrText := strings.TrimSpace(interception.OCRText); ocrText != "" {
		parts = append(parts, "OCR evidence:\n"+ocrText)
	}
	return strings.Join(parts, "\n\n")
}

func buildInterceptionSystemNote(interception *ChatInterception) string {
	if interception == nil {
		return ""
	}
	return " A motor-side context interception pass may provide OCR-derived evidence for attached images. Use it as grounding when relevant and avoid contradicting exact extracted facts."
}

func latestChatInterception(sessionID string, seq int32, fallback *ChatInterception) chatContextSnapshot {
	snapshot := defaultChatContextWorkingSet.Get(sessionID, seq)
	if snapshot.Interception == nil && fallback != nil {
		snapshot.Interception = fallback
	}
	return snapshot
}

func newOCRInterceptionClient(ctx context.Context) (diencephalon.Client, error) {
	cfg, ok, err := resolveOCRInterceptionConfig()
	if err != nil || !ok {
		return nil, err
	}
	return diencephalon.New(ctx, cfg)
}

func resolveOCRInterceptionConfig() (diencephalon.Config, bool, error) {
	cfg, err := appconfig.LoadDomourConfig()
	if err != nil {
		return diencephalon.Config{}, false, err
	}

	if explicit, ok := resolveExplicitOCRConfig(cfg); ok {
		if !supportsImageInterceptionProvider(explicit.Provider) {
			return diencephalon.Config{}, false, nil
		}
		return explicit, true, nil
	}

	chatCfg := diencephalon.ResolveConfig("chat", cfg)
	if strings.EqualFold(strings.TrimSpace(chatCfg.Provider), "ollama") && supportsImageInterceptionProvider(chatCfg.Provider) {
		chatCfg.APIKey = pickFirstNonEmpty(chatCfg.APIKey, "ollama")
		chatCfg.BaseURL = pickFirstNonEmpty(chatCfg.BaseURL, "http://127.0.0.1:11434/v1")
		return chatCfg, true, nil
	}

	if hasConfiguredProvider(cfg, "ollama") {
		return diencephalon.Config{
			Provider: "ollama",
			APIKey:   pickFirstNonEmpty(strings.TrimSpace(os.Getenv("DOMOUR_OCR_API_KEY")), cfg.APIKeyForProvider("ollama"), "ollama"),
			BaseURL:  pickFirstNonEmpty(strings.TrimSpace(os.Getenv("DOMOUR_OCR_BASE_URL")), cfg.BaseURLForProvider("ollama"), "http://127.0.0.1:11434/v1"),
			Model: pickFirstNonEmpty(
				strings.TrimSpace(os.Getenv("DOMOUR_OCR_MODEL")),
				cfg.EntryModel("ocr"),
				cfg.ProviderModel("ollama"),
				chatCfg.Model,
				cfg.DefaultModelName(),
			),
			ProxyURL: pickFirstNonEmpty(strings.TrimSpace(os.Getenv("DOMOUR_OCR_HTTPS_PROXY")), cfg.ProxyForProvider("ollama")),
		}, true, nil
	}

	if !supportsImageInterceptionProvider(chatCfg.Provider) {
		return diencephalon.Config{}, false, nil
	}
	return chatCfg, true, nil
}

func resolveExplicitOCRConfig(cfg appconfig.DomourConfig) (diencephalon.Config, bool) {
	envProvider := strings.TrimSpace(os.Getenv("DOMOUR_OCR_PROVIDER"))
	envModel := strings.TrimSpace(os.Getenv("DOMOUR_OCR_MODEL"))
	envBaseURL := strings.TrimSpace(os.Getenv("DOMOUR_OCR_BASE_URL"))
	envAPIKey := strings.TrimSpace(os.Getenv("DOMOUR_OCR_API_KEY"))
	envProxy := strings.TrimSpace(os.Getenv("DOMOUR_OCR_HTTPS_PROXY"))
	hasEnv := envProvider != "" || envModel != "" || envBaseURL != "" || envAPIKey != "" || envProxy != ""
	hasEntry := cfg.EntryProvider("ocr") != "" || cfg.EntryModel("ocr") != ""
	if !hasEnv && !hasEntry {
		return diencephalon.Config{}, false
	}

	resolved := diencephalon.ResolveConfig("ocr", cfg)
	resolved.Provider = pickFirstNonEmpty(envProvider, resolved.Provider)
	resolved.Model = pickFirstNonEmpty(envModel, resolved.Model)
	resolved.BaseURL = pickFirstNonEmpty(envBaseURL, resolved.BaseURL)
	resolved.APIKey = pickFirstNonEmpty(envAPIKey, resolved.APIKey)
	resolved.ProxyURL = pickFirstNonEmpty(envProxy, resolved.ProxyURL)
	if resolved.Provider == "" {
		return diencephalon.Config{}, false
	}
	if strings.EqualFold(resolved.Provider, "ollama") {
		resolved.APIKey = pickFirstNonEmpty(resolved.APIKey, "ollama")
		resolved.BaseURL = pickFirstNonEmpty(resolved.BaseURL, "http://127.0.0.1:11434/v1")
	}
	return resolved, true
}

func hasConfiguredProvider(cfg appconfig.DomourConfig, provider string) bool {
	return cfg.BaseURLForProvider(provider) != "" || cfg.APIKeyForProvider(provider) != "" || cfg.ProviderModel(provider) != ""
}

func supportsImageInterceptionProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "ollama", "gemini", "qwen":
		return true
	default:
		return false
	}
}

func pickFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (s *chatContextWorkingSet) Update(sessionID string, seq int32, patch *ChatInterception) chatContextSnapshot {
	if s == nil || patch == nil {
		return chatContextSnapshot{}
	}

	key := normalizedChatContextKey(sessionID, seq)

	s.mu.Lock()
	defer s.mu.Unlock()

	current, ok := s.getLocked(key)
	next := current
	next.Interception = mergeChatInterception(current.Interception, patch)
	if next.Interception == nil {
		return next.chatContextSnapshot
	}

	rawSignature := buildRawInterceptionSignature(next.Interception)
	if !ok || rawSignature != current.rawSignature {
		next.RawVersion++
	}
	next.rawSignature = rawSignature

	semanticSignature := buildSemanticInterceptionSignature(next.Interception)
	if !ok || semanticSignature != current.semanticSignature {
		next.SemanticVersion++
	}
	next.semanticSignature = semanticSignature

	s.setLocked(key, next)
	return next.chatContextSnapshot
}

func (s *chatContextWorkingSet) Get(sessionID string, seq int32) chatContextSnapshot {
	if s == nil {
		return chatContextSnapshot{}
	}

	key := normalizedChatContextKey(sessionID, seq)

	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.getLocked(key)
	if !ok {
		return chatContextSnapshot{}
	}
	return state.chatContextSnapshot
}

func (s *chatContextWorkingSet) Clear() {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cache != nil {
		s.cache.Clear()
	}
	s.data = map[string]chatContextState{}
}

func (s *chatContextWorkingSet) getLocked(key string) (chatContextState, bool) {
	if s.cache != nil {
		return s.cache.Get(key)
	}
	state, ok := s.data[key]
	return state, ok
}

func (s *chatContextWorkingSet) setLocked(key string, state chatContextState) {
	if s.cache != nil {
		s.cache.Set(key, state)
		return
	}
	if s.data == nil {
		s.data = map[string]chatContextState{}
	}
	s.data[key] = state
}

func normalizedChatContextKey(sessionID string, seq int32) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = defaultSessionID
	}
	return sessionID + "#" + strconv.Itoa(int(seq))
}

func mergeChatInterception(current, patch *ChatInterception) *ChatInterception {
	if current == nil && patch == nil {
		return nil
	}

	merged := &ChatInterception{}
	if current != nil {
		merged.Source = current.Source
		merged.Summary = current.Summary
		merged.OCRText = current.OCRText
		merged.KeyFacts = append([]string(nil), current.KeyFacts...)
	}
	if patch == nil {
		return merged
	}

	if source := strings.TrimSpace(patch.Source); source != "" {
		merged.Source = source
	}
	if summary := strings.TrimSpace(patch.Summary); summary != "" {
		merged.Summary = summary
	}
	if len(patch.KeyFacts) > 0 {
		merged.KeyFacts = normalizeKeyFacts(append(merged.KeyFacts, patch.KeyFacts...))
	}
	if ocrText := strings.TrimSpace(patch.OCRText); ocrText != "" {
		if len([]rune(ocrText)) >= len([]rune(strings.TrimSpace(merged.OCRText))) {
			merged.OCRText = ocrText
		}
	}
	if strings.TrimSpace(merged.Summary) == "" && strings.TrimSpace(merged.OCRText) == "" && len(merged.KeyFacts) == 0 {
		return nil
	}
	return merged
}

func normalizeKeyFacts(items []string) []string {
	if len(items) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		normalized := strings.Join(strings.Fields(strings.TrimSpace(item)), " ")
		if normalized == "" {
			continue
		}
		key := strings.ToLower(normalized)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func buildRawInterceptionSignature(interception *ChatInterception) string {
	if interception == nil {
		return ""
	}

	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(interception.Source)),
		strings.ToLower(strings.Join(strings.Fields(interception.Summary), " ")),
		strings.Join(normalizeKeyFacts(interception.KeyFacts), "|"),
		strings.ToLower(strings.Join(strings.Fields(truncateInterceptionText(interception.OCRText, 400)), " ")),
	}, "||")
}

func buildSemanticInterceptionSignature(interception *ChatInterception) string {
	if interception == nil {
		return ""
	}

	parts := []string{
		strings.ToLower(strings.Join(strings.Fields(interception.Summary), " ")),
		strings.Join(normalizeKeyFacts(interception.KeyFacts), "|"),
	}
	if len(interception.KeyFacts) == 0 {
		parts = append(parts, strings.ToLower(strings.Join(strings.Fields(truncateInterceptionText(interception.OCRText, 200)), " ")))
	}
	return strings.Join(parts, "||")
}

func resetChatContextWorkingSetForTest() {
	defaultChatContextWorkingSet.Clear()
}
