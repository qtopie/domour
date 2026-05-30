package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	autopilotpb "github.com/qtopie/domour/gen/assistant/autopilot"
	chatpb "github.com/qtopie/domour/gen/assistant/chat"
	copilotpb "github.com/qtopie/domour/gen/assistant/copilot"
	appconfig "github.com/qtopie/domour/internal/app/config"
	"github.com/qtopie/domour/internal/pkg/copilot/shared"
	providerruntime "github.com/qtopie/domour/internal/provider/runtime"
	"github.com/qtopie/domour/internal/session"
)

const defaultSessionID = "default-session"

// Server is the minimal built-in agent server behind chat/copilot/autopilot.
type Server struct {
	autopilotpb.UnimplementedAutopilotServiceServer
	chatpb.UnimplementedChatServiceServer
	copilotpb.UnimplementedCopilotServiceServer

	store session.Store
	brain BrainClient
	motor MotorClient
}

func NewServer(store session.Store) (*Server, error) {
	if store == nil {
		store = session.NewMemoryStore()
	}

	brain, err := newReloadableBrain()
	if err != nil {
		return nil, err
	}
	motorClient, err := newConfiguredMotorClient()
	if err != nil {
		return nil, err
	}

	return &Server{
		store: store,
		brain: brain,
		motor: motorClient,
	}, nil
}

func (s *Server) getSession(ctx context.Context, sessionID string) (session.Session, error) {
	if s.store == nil {
		return session.Session{
			ID:        sessionID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, nil
	}
	return s.store.GetSession(ctx, sessionID)
}

func (s *Server) appendHistory(ctx context.Context, sessionID, role, content string) error {
	return s.appendHistoryWithMeta(ctx, sessionID, role, content, "", "")
}

func (s *Server) appendHistoryWithMeta(ctx context.Context, sessionID, role, content, provider, model string) error {
	if s.store == nil {
		return nil
	}

	sess, err := s.getSession(ctx, sessionID)
	if err != nil {
		return err
	}

	var maxSeq int32 = 0
	for _, m := range sess.History {
		if m.Seq > maxSeq {
			maxSeq = m.Seq
		}
	}
	if sess.CompressedSeqMax > maxSeq {
		maxSeq = sess.CompressedSeqMax
	}
	newSeq := maxSeq + 1

	msg := shared.Message{
		Role:     role,
		Content:  content,
		Time:     time.Now().Unix(),
		Seq:      newSeq,
		Provider: provider,
		Model:    model,
	}

	sess.History = append(sess.History, msg)

	if provider != "" {
		sess.ActiveProvider = provider
	}
	if model != "" {
		sess.ActiveModel = model
	}

	if provider != "" {
		if sess.ProviderStats == nil {
			sess.ProviderStats = make(map[string]*session.ProviderStat)
		}
		stat, ok := sess.ProviderStats[provider]
		if !ok {
			stat = &session.ProviderStat{}
			sess.ProviderStats[provider] = stat
		}
		stat.CallCount++
		stat.TokenUsed += int64(EstimateTokenCount(content))
	}

	cfg, _ := appconfig.LoadDomourConfig()
	activeProvider := sess.ActiveProvider
	if activeProvider == "" {
		activeProvider = cfg.DefaultProviderName()
	}
	activeModel := sess.ActiveModel
	if activeModel == "" {
		activeModel = cfg.DefaultModelName()
	}

	_, compressTrigger := GetModelThresholds(cfg, activeProvider, activeModel)

	totalTokens := EstimateTokenCount(sess.MemorySummary)
	for _, m := range sess.History {
		totalTokens += EstimateTokenCount(m.Content)
	}

	if totalTokens > compressTrigger {
		_ = s.compressSessionIfNeeded(ctx, &sess)
	}

	return s.store.SaveSession(ctx, sess)
}

func (s *Server) getHistory(ctx context.Context, sessionID string) ([]shared.Message, error) {
	sess, err := s.getSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return sess.History, nil
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

func (s *Server) compressSessionIfNeeded(ctx context.Context, sess *session.Session) error {
	const KeepRecentMessages = 4
	if len(sess.History) <= KeepRecentMessages {
		return nil
	}

	numToCompress := len(sess.History) - KeepRecentMessages
	messagesToCompress := sess.History[:numToCompress]
	remainingMessages := sess.History[numToCompress:]

	var promptBuilder strings.Builder
	promptBuilder.WriteString("你是一个专业的高级记忆管理助手。请对以下对话进行压缩并整合进现有的记忆摘要中。\n\n")

	if sess.MemorySummary != "" {
		promptBuilder.WriteString("【已有的记忆摘要】:\n")
		promptBuilder.WriteString(sess.MemorySummary)
		promptBuilder.WriteString("\n\n")
	}

	promptBuilder.WriteString("【需要新增并整合进摘要的对话历史】:\n")
	for _, msg := range messagesToCompress {
		promptBuilder.WriteString(fmt.Sprintf("%s: %s\n", msg.Role, msg.Content))
	}

	promptBuilder.WriteString("\n【任务要求】:\n")
	promptBuilder.WriteString("1. 精炼并输出最新的全局会话摘要。控制在 200 字以内。\n")
	promptBuilder.WriteString("2. 重点保留：用户目的、涉及的文件路径、已有的技术选型/结论、核心配置、未完成的待办事项。\n")
	promptBuilder.WriteString("3. 不要包含任何客套话、解释或 Markdown Fences，只输出最终的纯文本摘要。")

	req := BrainChatRequest{
		SessionID:     sess.ID,
		Message:       promptBuilder.String(),
		History:       nil,
		MemorySummary: "",
		Provider:      sess.ActiveProvider,
		Model:         sess.ActiveModel,
	}

	resp, err := s.brain.ChatReply(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to generate session summary: %w", err)
	}

	sess.MemorySummary = strings.TrimSpace(resp.Content)
	sess.History = remainingMessages

	if len(messagesToCompress) > 0 {
		sess.CompressedSeqMax = messagesToCompress[len(messagesToCompress)-1].Seq
	}

	return nil
}

func normalizeSessionID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return defaultSessionID
	}
	return sessionID
}

func firstNonEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func withRuntimeMetadata(ctx context.Context, sessionID, workspace string) context.Context {
	return providerruntime.WithRequestMetadata(ctx, providerruntime.RequestMetadata{
		SessionID: sessionID,
		Workspace: strings.TrimSpace(workspace),
	})
}

func resolveCopilotMode(message string) string {
	lower := strings.ToLower(strings.TrimSpace(message))
	switch {
	case strings.Contains(lower, "[active]"), strings.Contains(lower, "积极模式"), strings.Contains(lower, "/active"):
		return "active"
	case strings.Contains(lower, "[normal]"), strings.Contains(lower, "普通模式"), strings.Contains(lower, "/normal"):
		return "normal"
	}

	mode := strings.ToLower(strings.TrimSpace(os.Getenv("DOMOUR_COPILOT_MODE")))
	if mode == "active" {
		return "active"
	}
	return "normal"
}

func (s *Server) streamBrainToBridge(ctx context.Context, cancel context.CancelFunc, req BrainChatRequest, bridge *SessionBridge) {
	defer close(bridge.BrainOut)

	brainStream, err := s.brain.StreamChat(ctx, req)
	if err != nil {
		bridge.BrainOut <- BrainStreamEvent{Type: "error", Err: err}
		return
	}

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() != context.Canceled {
				bridge.BrainOut <- BrainStreamEvent{Type: "error", Err: ctx.Err()}
			}
			return
		case control := <-bridge.Control:
			switch control.Type {
			case "stop", "refuse":
				cancel()
				return
			}
		case event, ok := <-brainStream:
			if !ok {
				return
			}
			bridge.BrainOut <- event
			if event.Err != nil {
				return
			}
		}
	}
}

func (s *Server) streamMotorToBridge(ctx context.Context, req MotorChatRequest, bridge *SessionBridge) {
	_ = s.motor.StreamChat(ctx, req, bridge)
}

func (s *Server) streamAutopilotBrainToBridge(ctx context.Context, cancel context.CancelFunc, req BrainAutopilotRequest, bridge *SessionBridge) {
	defer close(bridge.BrainOut)

	brainStream, err := s.brain.StreamAutopilot(ctx, req)
	if err != nil {
		bridge.BrainOut <- BrainStreamEvent{Type: "error", Err: err}
		return
	}

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() != context.Canceled {
				bridge.BrainOut <- BrainStreamEvent{Type: "error", Err: ctx.Err()}
			}
			return
		case control := <-bridge.Control:
			switch control.Type {
			case "stop", "refuse":
				cancel()
				return
			}
		case event, ok := <-brainStream:
			if !ok {
				return
			}
			bridge.BrainOut <- event
			if event.Err != nil {
				return
			}
		}
	}
}

func (s *Server) streamCopilotBrainToBridge(ctx context.Context, cancel context.CancelFunc, req BrainCopilotRequest, bridge *SessionBridge) {
	defer close(bridge.BrainOut)

	brainStream, err := s.brain.StreamCopilot(ctx, req)
	if err != nil {
		bridge.BrainOut <- BrainStreamEvent{Type: "error", Err: err}
		return
	}

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() != context.Canceled {
				bridge.BrainOut <- BrainStreamEvent{Type: "error", Err: ctx.Err()}
			}
			return
		case control := <-bridge.Control:
			switch control.Type {
			case "stop", "refuse":
				cancel()
				return
			}
		case event, ok := <-brainStream:
			if !ok {
				return
			}
			bridge.BrainOut <- event
			if event.Err != nil {
				return
			}
		}
	}
}
