package supervisor

import (
	"context"
	"fmt"
)

// InspectResult 表示 Inspect 查询命中的结果
type InspectResult struct {
	ProviderName   string
	SessionID      string
	TranscriptPath string
	Records        []*TranscriptRecord
}

// MatchProviderNameOrAlias 判断给定的 provider 标识或别名是否与实际 Provider 名称匹配
func MatchProviderNameOrAlias(actualName, queryName string) bool {
	if actualName == queryName {
		return true
	}
	switch actualName {
	case "chatgpt":
		return queryName == "codex" || queryName == "openai"
	case "antigravity":
		return queryName == "agy"
	}
	return false
}

// InspectSession 查询指定 sessionID 的对话记录。
// 当 explicitProvider 不为空时，定向路由至该 Provider；
// 当 explicitProvider 为空时，自动遍历已注册的 providers 进行智能特征匹配 (Smart Routing)。
func InspectSession(providers []AgentProvider, explicitProvider string, sessionID string, maxTurns int) (*InspectResult, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("sessionID cannot be empty")
	}
	if maxTurns <= 0 {
		maxTurns = 5
	}

	if explicitProvider != "" {
		var targetProvider AgentProvider
		for _, p := range providers {
			if MatchProviderNameOrAlias(p.Name(), explicitProvider) {
				targetProvider = p
				break
			}
		}
		if targetProvider == nil {
			return nil, fmt.Errorf("provider %q not found or not registered", explicitProvider)
		}

		path, ok := targetProvider.MatchSession(sessionID)
		if !ok {
			return nil, fmt.Errorf("session %s not found in provider %s", sessionID, explicitProvider)
		}

		records, err := targetProvider.ReadRecentTurns(sessionID, maxTurns)
		if err != nil {
			return nil, fmt.Errorf("failed to read turns from provider %s: %w", explicitProvider, err)
		}

		return &InspectResult{
			ProviderName:   targetProvider.Name(),
			SessionID:      sessionID,
			TranscriptPath: path,
			Records:        records,
		}, nil
	}

	// 自动探测路由 (Smart Auto-Discovery Routing)
	type matchItem struct {
		provider AgentProvider
		path     string
	}
	var matched []matchItem

	for _, p := range providers {
		if path, ok := p.MatchSession(sessionID); ok {
			matched = append(matched, matchItem{
				provider: p,
				path:     path,
			})
		}
	}

	if len(matched) == 0 {
		return nil, fmt.Errorf("session %s not found in any registered provider", sessionID)
	}

	if len(matched) > 1 {
		names := make([]string, 0, len(matched))
		for _, m := range matched {
			names = append(names, m.provider.Name())
		}
		return nil, fmt.Errorf("ambiguous session %s matched multiple providers (%v), please specify -p <provider>", sessionID, names)
	}

	// 唯一命中
	hit := matched[0]
	records, err := hit.provider.ReadRecentTurns(sessionID, maxTurns)
	if err != nil {
		return nil, fmt.Errorf("failed to read turns from matched provider %s: %w", hit.provider.Name(), err)
	}

	return &InspectResult{
		ProviderName:   hit.provider.Name(),
		SessionID:      sessionID,
		TranscriptPath: hit.path,
		Records:        records,
	}, nil
}

// InspectSession 实现 Supervisor 门面上的 InspectSession 方法
func (s *defaultSupervisor) InspectSession(ctx context.Context, explicitProvider string, sessionID string, maxTurns int) (*InspectResult, error) {
	s.mu.RLock()
	providerList := make([]AgentProvider, 0, len(s.providers))
	for _, p := range s.providers {
		providerList = append(providerList, p)
	}
	s.mu.RUnlock()

	return InspectSession(providerList, explicitProvider, sessionID, maxTurns)
}
