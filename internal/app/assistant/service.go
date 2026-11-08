package assistant

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	domourmodel "github.com/qtopie/domour/ark/cognitor"
	"github.com/qtopie/domour/ark/session"
	runtimespi "github.com/qtopie/domour/ark/spi/runtime"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	bioniccontext "github.com/qtopie/domour/internal/bionic/context"
	"github.com/qtopie/domour/internal/cognitor/proxy"
	appconfig "github.com/qtopie/domour/internal/config"
	"github.com/qtopie/domour/internal/engine"
	"github.com/qtopie/domour/internal/infra/eventbus"
	providerruntime "github.com/qtopie/domour/internal/infra/llm/runtime"
)

type AssistantService struct {
	engine           engine.Engine
	store            session.Store
	locker           session.Locker
	interceptor      bioniccontext.ChatContextInterceptor
	eb               eventbus.EventBus
	orchestrator     engine.AgentOrchestrator
	router           runtimespi.Router
	compressInFlight sync.Map
}

type AssistantServiceOption func(*AssistantService)

func WithServiceOrchestrator(orch engine.AgentOrchestrator) AssistantServiceOption {
	return func(s *AssistantService) {
		if orch != nil {
			s.orchestrator = orch
		}
	}
}

func WithServiceEventBus(eb eventbus.EventBus) AssistantServiceOption {
	return func(s *AssistantService) {
		if eb != nil {
			s.eb = eb
		}
	}
}

func WithServiceRouter(r runtimespi.Router) AssistantServiceOption {
	return func(s *AssistantService) {
		if r != nil {
			s.router = r
		}
	}
}

func NewAssistantService(
	eng engine.Engine,
	store session.Store,
	opts ...AssistantServiceOption,
) *AssistantService {
	var eb eventbus.EventBus
	var orch engine.AgentOrchestrator
	if eng != nil {
		eb = eng.EventBus()
		orch = eng.Orchestrator()
	}
	s := &AssistantService{
		engine:       eng,
		store:        store,
		locker:       session.NewLocalLocker(),
		interceptor:  bioniccontext.NewChatContextInterceptor(),
		eb:           eb,
		orchestrator: orch,
		router:       runtimespi.NewDefaultDualPathwayRouter(),
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.eb != nil && eng != nil && s.orchestrator == eng.Orchestrator() && s.eb != eng.EventBus() {
		s.orchestrator = engine.NewLocalOrchestrator(eng.Cognitor(), eng.Executor(), s.eb)
	}
	return s
}

func (s *AssistantService) Router() runtimespi.Router {
	return s.router
}

func (s *AssistantService) Store() session.Store {
	return s.store
}

func (s *AssistantService) EventBus() eventbus.EventBus {
	return s.eb
}

func (s *AssistantService) Orchestrator() engine.AgentOrchestrator {
	return s.orchestrator
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

	if err := s.store.SaveSession(ctx, sess); err != nil {
		return err
	}

	if totalTokens > compressTrigger {
		if _, loaded := s.compressInFlight.LoadOrStore(sessionID, struct{}{}); !loaded {
			bgCtx := context.WithoutCancel(ctx)
			go func(sid string) {
				defer s.compressInFlight.Delete(sid)
				cCtx, cancel := context.WithTimeout(bgCtx, 30*time.Second)
				defer cancel()
				latestSess, getErr := s.GetSession(cCtx, sid)
				if getErr != nil {
					return
				}
				if compErr := s.compressSessionIfNeeded(cCtx, &latestSess); compErr == nil {
					_ = s.store.SaveSession(cCtx, latestSess)
				}
			}(sessionID)
		}
	}

	return nil
}

// RebindOptions dynamically updates the service's store and optional SPI components.
// Used when the host registers gRPC services during Provision and injects cluster-aware
// store/eventbus/router/orchestrator during Start.
func (s *AssistantService) RebindOptions(store session.Store, opts ...AssistantServiceOption) {
	if store != nil {
		s.store = store
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.eb != nil && s.engine != nil {
		if s.orchestrator == nil || s.orchestrator == s.engine.Orchestrator() {
			s.orchestrator = engine.NewLocalOrchestrator(s.engine.Cognitor(), s.engine.Executor(), s.eb)
		} else if lo, ok := s.orchestrator.(*engine.LocalOrchestrator); ok && lo.EventBus() != s.eb {
			s.orchestrator = engine.NewLocalOrchestrator(s.engine.Cognitor(), s.engine.Executor(), s.eb)
		}
	}
}

func ensureSessionContext(ctx context.Context, sessionID, workspace string) context.Context {
	rmeta := providerruntime.RequestMetadataFromContext(ctx)
	if rmeta.SessionID == sessionID && (workspace == "" || rmeta.Workspace != "") {
		return ctx
	}
	if rmeta.SessionID == "" {
		rmeta.SessionID = sessionID
	}
	if rmeta.Workspace == "" && workspace != "" {
		rmeta.Workspace = strings.TrimSpace(workspace)
	}
	if rmeta.Mode == "" {
		rmeta.Mode = "balanced"
	}
	return providerruntime.WithRequestMetadata(ctx, rmeta)
}

func mergeUniqueStrings(slices ...[]string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, sl := range slices {
		for _, item := range sl {
			trimmed := strings.TrimSpace(item)
			if trimmed == "" {
				continue
			}
			if _, exists := seen[trimmed]; !exists {
				seen[trimmed] = struct{}{}
				out = append(out, trimmed)
			}
		}
	}
	return out
}

// routeAndResolveModel consults s.router.Route and resolves the best provider/model
// combining caller overrides, route decision tags, system mode tags, and session stickiness.
func (s *AssistantService) routeAndResolveModel(
	ctx context.Context,
	sessionID string,
	message string,
	entry string,
	sess session.Session,
	callerProvider string,
	callerModel string,
) (runtimespi.RouteDecision, string, string) {
	rmeta := providerruntime.RequestMetadataFromContext(ctx)
	routeMeta := map[string]string{
		"entry": entry,
		"mode":  rmeta.Mode,
	}

	routeDec := runtimespi.RouteDecision{
		TargetCore: runtimespi.CorePrimary,
		Mode:       "react",
	}
	if s.router != nil {
		if dec, err := s.router.Route(ctx, sessionID, message, routeMeta); err == nil {
			if dec.Mode == "" {
				dec.Mode = "react"
			}
			routeDec = dec
		}
	}

	provider := strings.TrimSpace(callerProvider)
	model := strings.TrimSpace(callerModel)

	// If caller explicitly specified both provider and model, honor it directly.
	if provider != "" && model != "" {
		return routeDec, provider, model
	}

	hasRouteTier := routeDec.FastPath || len(routeDec.RequiredTags) > 0 || routeDec.Mode == "planner"

	// Session stickiness: if session already has a healthy active provider and the router
	// did not request a specific fast/deep tier switch, prefer the session's active model.
	if !hasRouteTier && provider == "" && sess.ActiveProvider != "" && proxy.IsProviderHealthy(sess.ActiveProvider) {
		provider = sess.ActiveProvider
		if model == "" {
			model = sess.ActiveModel
		}
	}

	// Query ModelRegistry using combined system mode + route decision tags when provider/model is still unresolved.
	if provider == "" || model == "" {
		cfg, cfgErr := appconfig.LoadDomourConfig()
		if cfgErr == nil {
			mm := cfg.ModeMapping(rmeta.Mode)
			reqTags := mergeUniqueStrings(mm.Require, routeDec.RequiredTags)
			prefTags := mergeUniqueStrings(routeDec.PreferTags, mm.Prefer)

			if p, m, qErr := domourmodel.DefaultRegistry().QueryBestModel(reqTags, prefTags, mm.Exclude); qErr == nil && p != "" && proxy.IsProviderHealthy(p) {
				if provider == "" {
					provider = p
				}
				if model == "" {
					model = m
				}
			} else if len(routeDec.RequiredTags) > 0 {
				// Relax strict RequiredTags from router if no model matched in registry
				if p, m, qErr2 := domourmodel.DefaultRegistry().QueryBestModel(mm.Require, prefTags, mm.Exclude); qErr2 == nil && p != "" && proxy.IsProviderHealthy(p) {
					if provider == "" {
						provider = p
					}
					if model == "" {
						model = m
					}
				}
			}
		}
	}

	// Final fallback to session stickiness if still empty
	if provider == "" && sess.ActiveProvider != "" && proxy.IsProviderHealthy(sess.ActiveProvider) {
		provider = sess.ActiveProvider
		if model == "" {
			model = sess.ActiveModel
		}
	}

	return routeDec, provider, model
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


