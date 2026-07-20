package context

import (
	"github.com/qtopie/domour/internal/infra/llm"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"context"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	appconfig "github.com/qtopie/domour/internal/config"
	"github.com/qtopie/domour/pkg/infra/cache/l1"
	"github.com/qtopie/domour/internal/cognitor/proxy"
)

var (
	chatOCRContextTimeout       = 2 * time.Second
	MaxChatContextRefreshRounds = 3
	ocrConfidenceThreshold      = 0.6 // Ignore OCR if confidence is below this

	DefaultChatContextWorkingSet = newChatContextWorkingSet(1024, 15*time.Minute)
)

type ChatContextInterceptor interface {
	InterceptChatContext(ctx context.Context, req shared.MotorChatRequest) (*shared.ChatInterception, error)
}

type llmChatContextInterceptor struct{}

type ChatContextSnapshot struct {
	Interception    *shared.ChatInterception
	RawVersion      int64
	SemanticVersion int64
}

type chatContextState struct {
	ChatContextSnapshot
	rawSignature      string
	semanticSignature string
}

type chatContextWorkingSet struct {
	mu    sync.Mutex
	cache *l1.Cache[string, chatContextState]
	data  map[string]chatContextState
}

func NewChatContextInterceptor() ChatContextInterceptor {
	return &llmChatContextInterceptor{}
}

func newChatContextWorkingSet(capacity int, ttl time.Duration) *chatContextWorkingSet {
	cache, err := l1.NewCache[string, chatContextState](capacity, ttl)
	if err != nil {
		return &chatContextWorkingSet{data: map[string]chatContextState{}}
	}
	return &chatContextWorkingSet{cache: cache}
}

func (i *llmChatContextInterceptor) InterceptChatContext(ctx context.Context, req shared.MotorChatRequest) (*shared.ChatInterception, error) {
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
	userMessage, err := llm.BuildUserInputMessage(buildOCRInterceptionPrompt(req.Message), attachments)
	if err != nil {
		return nil, err
	}
	messages = append(messages, userMessage)

	reply, err := client.GenerateText(ocrCtx, messages)
	if err != nil {
		return nil, err
	}

	interception := parseChatInterception(shared.StripCodeFence(reply.Content))
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
		"CONFIDENCE_SCORE:",
		"In SUMMARY, write one short sentence describing the document or image type.",
		"In KEY_FACTS, list exact high-risk facts visible in the image such as numbers, dates, totals, IDs, names, labels, and short field/value pairs.",
		"In OCR_TEXT, include the most relevant visible text verbatim in natural reading order.",
		"In CONFIDENCE_SCORE, provide a numerical value from 0.0 to 1.0 representing your certainty of the text extraction accuracy.",
		"Do not add markdown fences.",
	}
	if shared.WantsOCRTask(message) {
		parts = append(parts, "The user explicitly wants OCR. Be more exhaustive in OCR_TEXT and preserve structure when possible.")
	}
	return strings.Join(parts, "\n")
}

func parseChatInterception(content string) *shared.ChatInterception {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	result := &shared.ChatInterception{
		Confidence: 1.0, // Default to 1.0 if not specified
	}
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
		case "CONFIDENCE_SCORE":
			current = "confidence"
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
		case "confidence":
			if val, err := strconv.ParseFloat(line, 64); err == nil {
				result.Confidence = val
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

func ApplyChatInterceptionContext(prompt string, interception *shared.ChatInterception) string {
	if interception == nil {
		return prompt
	}

	// Drop low-confidence OCR evidence
	if interception.Confidence < ocrConfidenceThreshold {
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

func BuildInterceptionSystemNote(interception *shared.ChatInterception) string {
	if interception == nil {
		return ""
	}
	return " A motor-side context interception pass may provide OCR-derived evidence for attached images. Use it as grounding when relevant and avoid contradicting exact extracted facts."
}

func LatestChatInterception(sessionID string, seq int32, fallback *shared.ChatInterception) ChatContextSnapshot {
	snapshot := DefaultChatContextWorkingSet.Get(sessionID, seq)
	if snapshot.Interception == nil && fallback != nil {
		snapshot.Interception = fallback
	}
	return snapshot
}

func newOCRInterceptionClient(ctx context.Context) (*proxy.Client, error) {
	cfg, ok, err := resolveOCRInterceptionConfig()
	if err != nil || !ok {
		return nil, err
	}
	return proxy.New(ctx, cfg)
}

func resolveOCRInterceptionConfig() (proxy.Config, bool, error) {
	cfg, err := appconfig.LoadDomourConfig()
	if err != nil {
		return proxy.Config{}, false, err
	}

	if explicit, ok := resolveExplicitOCRConfig(cfg); ok {
		if !supportsImageInterceptionProvider(explicit.Provider) {
			return proxy.Config{}, false, nil
		}
		return explicit, true, nil
	}

	chatCfg := proxy.ResolveConfig("chat", cfg)
	if (strings.EqualFold(strings.TrimSpace(chatCfg.Provider), "ollama") || strings.EqualFold(strings.TrimSpace(chatCfg.Provider), "llamacpp")) && supportsImageInterceptionProvider(chatCfg.Provider) {
		chatCfg.APIKey = pickFirstNonEmpty(chatCfg.APIKey, "llamacpp")
		chatCfg.BaseURL = pickFirstNonEmpty(chatCfg.BaseURL, "http://127.0.0.1:38082/v1")
		return chatCfg, true, nil
	}

	if hasConfiguredProvider(cfg, "ollama") || hasConfiguredProvider(cfg, "llamacpp") {
		ocProvider := "ollama"
		if hasConfiguredProvider(cfg, "llamacpp") {
			ocProvider = "llamacpp"
		}
		return proxy.Config{
			Provider: ocProvider,
			APIKey:   pickFirstNonEmpty(strings.TrimSpace(os.Getenv("DOMOUR_OCR_API_KEY")), cfg.APIKeyForProvider(ocProvider), "llamacpp"),
			BaseURL:  pickFirstNonEmpty(strings.TrimSpace(os.Getenv("DOMOUR_OCR_BASE_URL")), cfg.BaseURLForProvider(ocProvider), "http://127.0.0.1:38082/v1"),
			Model: pickFirstNonEmpty(
				strings.TrimSpace(os.Getenv("DOMOUR_OCR_MODEL")),
				cfg.EntryModel("ocr"),
				cfg.ProviderModel(ocProvider),
				chatCfg.Model,
				cfg.DefaultModelName(),
			),
			ProxyURL: pickFirstNonEmpty(strings.TrimSpace(os.Getenv("DOMOUR_OCR_HTTPS_PROXY")), cfg.ProxyForProvider(ocProvider)),
		}, true, nil
	}

	if !supportsImageInterceptionProvider(chatCfg.Provider) {
		return proxy.Config{}, false, nil
	}
	return chatCfg, true, nil
}

func resolveExplicitOCRConfig(cfg appconfig.DomourConfig) (proxy.Config, bool) {
	envProvider := strings.TrimSpace(os.Getenv("DOMOUR_OCR_PROVIDER"))
	envModel := strings.TrimSpace(os.Getenv("DOMOUR_OCR_MODEL"))
	envBaseURL := strings.TrimSpace(os.Getenv("DOMOUR_OCR_BASE_URL"))
	envAPIKey := strings.TrimSpace(os.Getenv("DOMOUR_OCR_API_KEY"))
	envProxy := strings.TrimSpace(os.Getenv("DOMOUR_OCR_HTTPS_PROXY"))
	hasEnv := envProvider != "" || envModel != "" || envBaseURL != "" || envAPIKey != "" || envProxy != ""
	hasEntry := cfg.EntryProvider("ocr") != "" || cfg.EntryModel("ocr") != ""
	if !hasEnv && !hasEntry {
		return proxy.Config{}, false
	}

	resolved := proxy.ResolveConfig("ocr", cfg)
	resolved.Provider = pickFirstNonEmpty(envProvider, resolved.Provider)
	resolved.Model = pickFirstNonEmpty(envModel, resolved.Model)
	resolved.BaseURL = pickFirstNonEmpty(envBaseURL, resolved.BaseURL)
	resolved.APIKey = pickFirstNonEmpty(envAPIKey, resolved.APIKey)
	resolved.ProxyURL = pickFirstNonEmpty(envProxy, resolved.ProxyURL)
	if resolved.Provider == "" {
		return proxy.Config{}, false
	}
	if strings.EqualFold(resolved.Provider, "ollama") || strings.EqualFold(resolved.Provider, "llamacpp") {
		ocAPIKey := "llamacpp"
		ocBaseURL := "http://127.0.0.1:38082/v1"
		if strings.EqualFold(resolved.Provider, "llamacpp") {
			ocAPIKey = "llamacpp"
			ocBaseURL = "http://127.0.0.1:38082/v1"
		}
		resolved.APIKey = pickFirstNonEmpty(resolved.APIKey, ocAPIKey)
		resolved.BaseURL = pickFirstNonEmpty(resolved.BaseURL, ocBaseURL)
	}
	return resolved, true
}

func hasConfiguredProvider(cfg appconfig.DomourConfig, provider string) bool {
	return cfg.BaseURLForProvider(provider) != "" || cfg.APIKeyForProvider(provider) != "" || cfg.ProviderModel(provider) != ""
}

func supportsImageInterceptionProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "ollama", "llamacpp", "gemini", "qwen", "deepseek":
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

func (s *chatContextWorkingSet) Update(sessionID string, seq int32, patch *shared.ChatInterception) ChatContextSnapshot {
	if s == nil || patch == nil {
		return ChatContextSnapshot{}
	}

	key := normalizedChatContextKey(sessionID, seq)

	s.mu.Lock()
	defer s.mu.Unlock()

	current, ok := s.getLocked(key)
	next := current
	next.Interception = mergeChatInterception(current.Interception, patch)
	if next.Interception == nil {
		return next.ChatContextSnapshot
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
	return next.ChatContextSnapshot
}

func (s *chatContextWorkingSet) Get(sessionID string, seq int32) ChatContextSnapshot {
	if s == nil {
		return ChatContextSnapshot{}
	}

	key := normalizedChatContextKey(sessionID, seq)

	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.getLocked(key)
	if !ok {
		return ChatContextSnapshot{}
	}
	return state.ChatContextSnapshot
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
		sessionID = shared.DefaultSessionID
	}
	return sessionID + "#" + strconv.Itoa(int(seq))
}

func mergeChatInterception(current, patch *shared.ChatInterception) *shared.ChatInterception {
	if current == nil && patch == nil {
		return nil
	}

	merged := &shared.ChatInterception{
		Confidence: 1.0,
	}
	if current != nil {
		merged.Source = current.Source
		merged.Summary = current.Summary
		merged.OCRText = current.OCRText
		merged.KeyFacts = append([]string(nil), current.KeyFacts...)
		merged.Confidence = current.Confidence
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
	if patch.Confidence > 0 {
		merged.Confidence = patch.Confidence
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

func buildRawInterceptionSignature(interception *shared.ChatInterception) string {
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

func buildSemanticInterceptionSignature(interception *shared.ChatInterception) string {
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
	DefaultChatContextWorkingSet.Clear()
}
