package supervisor

import (
	"context"
	"fmt"
	"strings"
)

// DiscoverSessions 汇总发现当前系统所有运行中或本地存在的活跃 Agent 会话
func (s *defaultSupervisor) DiscoverSessions(ctx context.Context) ([]*AgentProcess, error) {
	s.mu.RLock()
	providers := make([]AgentProvider, 0, len(s.providers))
	for _, p := range s.providers {
		providers = append(providers, p)
	}
	s.mu.RUnlock()

	resultMap := make(map[string]*AgentProcess)

	// 1. 扫描底层所有已注册 Provider
	for _, p := range providers {
		discovered, err := p.DiscoverSessions()
		if err == nil {
			for _, proc := range discovered {
				if proc != nil && proc.ID != "" {
					resultMap[proc.ID] = proc
				}
			}
		}
	}

	// 2. 结合内存中由 Supervisor 直接托管的进程
	for _, proc := range s.registry.List() {
		if _, exists := resultMap[proc.ID]; !exists {
			resultMap[proc.ID] = proc
		}
	}

	results := make([]*AgentProcess, 0, len(resultMap))
	for _, p := range resultMap {
		results = append(results, p)
	}

	return results, nil
}

// SpawnSession 统一拉起全新的 Agent 任务，多阶梯自适应判定
func (s *defaultSupervisor) SpawnSession(ctx context.Context, req SpawnRequest) (*SpawnResult, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("prompt cannot be empty")
	}

	s.mu.RLock()
	var targetProvider AgentProvider
	if req.Provider != "" {
		targetProvider = s.providers[req.Provider]
	} else {
		// 默认优先选择 antigravity，或者列表中的首个 provider
		if p, ok := s.providers["antigravity"]; ok {
			targetProvider = p
		} else {
			for _, p := range s.providers {
				targetProvider = p
				break
			}
		}
	}
	s.mu.RUnlock()

	if targetProvider == nil {
		return nil, fmt.Errorf("no suitable agent provider registered for spawn")
	}

	return targetProvider.SpawnSession(ctx, req)
}

// InstructSession 统一向运行中或已存在的会话下发后续指令
func (s *defaultSupervisor) InstructSession(ctx context.Context, req InstructRequest) (*InstructResult, error) {
	if req.SessionID == "" {
		return nil, fmt.Errorf("session_id cannot be empty")
	}
	if strings.TrimSpace(req.Instruction) == "" {
		return nil, fmt.Errorf("instruction cannot be empty")
	}

	// 阶梯 1: 尝试通过已连接的 Controller 长连接管道发送
	if s.controller != nil {
		if err := s.controller.SendInstruction(ctx, req.SessionID, req.Instruction); err == nil {
			return &InstructResult{
				SessionID:   req.SessionID,
				Status:      "sent",
				Mode:        "stream_pipe",
				Instruction: req.Instruction,
			}, nil
		}
	}

	s.mu.RLock()
	var targetProvider AgentProvider
	if req.Provider != "" {
		targetProvider = s.providers[req.Provider]
	} else {
		// 自动探测该 sessionID 归属的 provider
		for _, p := range s.providers {
			if _, ok := p.MatchSession(req.SessionID); ok {
				targetProvider = p
				break
			}
		}
		if targetProvider == nil {
			// 回退到默认 antigravity
			targetProvider = s.providers["antigravity"]
		}
	}
	s.mu.RUnlock()

	if targetProvider == nil {
		return nil, fmt.Errorf("no suitable provider found to instruct session %s", req.SessionID)
	}

	return targetProvider.InstructSession(ctx, req)
}
