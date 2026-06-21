package assistant

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	bioniccontext "github.com/qtopie/domour/internal/bionic/context"
	"github.com/qtopie/domour/pkg/bionic/session"
	appconfig "github.com/qtopie/domour/internal/config"
	"github.com/qtopie/domour/internal/engine"
	"github.com/qtopie/domour/internal/infra/dapr"
	"github.com/qtopie/domour/internal/infra/eventbus"
)

type AssistantService struct {
	engine       engine.Engine
	store        session.Store
	locker       session.Locker
	interceptor  bioniccontext.ChatContextInterceptor
	eb           eventbus.EventBus
	orchestrator dapr.DurableAgentOrchestrator
}

func NewAssistantService(
	engine engine.Engine,
	store session.Store,
	eb eventbus.EventBus,
	orchestrator dapr.DurableAgentOrchestrator,
) *AssistantService {
	return &AssistantService{
		engine:       engine,
		store:        store,
		locker:       session.NewLocalLocker(),
		interceptor:  bioniccontext.NewChatContextInterceptor(),
		eb:           eb,
		orchestrator: orchestrator,
	}
}

func (s *AssistantService) Engine() engine.Engine {
	return s.engine
}

func (s *AssistantService) Locker() session.Locker {
	return s.locker
}

func (s *AssistantService) GetSession(ctx context.Context, sessionID string) (session.Session, error) {
	if s.store == nil {
		return session.Session{
			ID:        sessionID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, nil
	}

	sess, err := s.store.GetSession(ctx, sessionID)
	if err == nil && len(sess.History) > 0 {
		return sess, nil
	}

	// Try to lazy-load from the unified query service (which scans local CLI logs)
	infos, queryErr := session.QuerySessions(ctx, s.store, session.QueryFilter{SessionID: sessionID})
	if queryErr == nil && len(infos) > 0 {
		info := infos[0]
		loadedSess := session.Session{
			ID:             info.SessionID,
			ActiveProvider: info.Provider,
			ActiveModel:    info.Model,
			History:        info.History,
			CreatedAt:      info.UpdatedAt,
			UpdatedAt:      info.UpdatedAt,
		}
		_ = s.store.SaveSession(ctx, loadedSess)
		return loadedSess, nil
	}

	return sess, nil
}

func (s *AssistantService) AppendHistory(ctx context.Context, sessionID, role, content string) error {
	return s.AppendHistoryWithMeta(ctx, sessionID, role, content, "", "")
}

func (s *AssistantService) AppendHistoryWithMeta(ctx context.Context, sessionID, role, content, provider, model string) error {
	if s.store == nil {
		return nil
	}

	sess, err := s.GetSession(ctx, sessionID)
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

func (s *AssistantService) compressSessionIfNeeded(ctx context.Context, sess *session.Session) error {
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

	brainClient, err := s.engine.Cognitor().GetClientWithOverride(ctx, "chat", sess.ActiveProvider, sess.ActiveModel)
	if err != nil {
		return fmt.Errorf("failed to resolve brain client for session compression: %w", err)
	}

	messages := []*schema.Message{
		schema.SystemMessage(promptBuilder.String()),
	}

	resp, err := brainClient.GenerateText(ctx, messages)
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


