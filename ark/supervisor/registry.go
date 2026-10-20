package supervisor

import (
	"sync"
	"time"
)

// ProcessEventType 实例事件类型
type ProcessEventType string

const (
	EventProcessDiscovered ProcessEventType = "discovered"
	EventProcessUpdated    ProcessEventType = "updated"
	EventProcessToolPending ProcessEventType = "tool_pending"
	EventProcessTerminated ProcessEventType = "terminated"
)

// ProcessEvent 实例变更通知事件
type ProcessEvent struct {
	Type    ProcessEventType
	Process *AgentProcess
}

// Registry 管理所有本地被发现或托管的 Agent 实例
type Registry struct {
	mu        sync.RWMutex
	processes map[string]*AgentProcess
	listeners []chan ProcessEvent
}

// NewRegistry 创建一个空注册表
func NewRegistry() *Registry {
	return &Registry{
		processes: make(map[string]*AgentProcess),
		listeners: make([]chan ProcessEvent, 0),
	}
}

// Subscribe 订阅实例事件
func (r *Registry) Subscribe(ch chan ProcessEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listeners = append(r.listeners, ch)
}

// Unsubscribe 取消订阅
func (r *Registry) Unsubscribe(ch chan ProcessEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, l := range r.listeners {
		if l == ch {
			r.listeners = append(r.listeners[:i], r.listeners[i+1:]...)
			break
		}
	}
}

func (r *Registry) notify(event ProcessEvent) {
	for _, ch := range r.listeners {
		select {
		case ch <- event:
		default:
		}
	}
}

// Register 显式注册或更新进程实体
func (r *Registry) Register(p *AgentProcess) {
	r.mu.Lock()
	defer r.mu.Unlock()

	p.LastHeartbeat = time.Now()
	_, exists := r.processes[p.ID]
	r.processes[p.ID] = p

	eventType := EventProcessDiscovered
	if exists {
		eventType = EventProcessUpdated
	}
	r.notify(ProcessEvent{
		Type:    eventType,
		Process: p,
	})
}

// UpdateFromStatusline 接收来自状态栏的心跳增量更新
func (r *Registry) UpdateFromStatusline(update *StatuslineUpdate, provider string) *AgentProcess {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, exists := r.processes[update.ID]
	if !exists {
		p = &AgentProcess{
			ID:       update.ID,
			PID:      update.PID,
			Provider: provider,
			Status:   StatusInitializing,
		}
		r.processes[update.ID] = p
	}

	if update.PID > 0 {
		p.PID = update.PID
	}
	if update.Workspace != "" {
		p.Workspace = update.Workspace
	}
	if update.TranscriptPath != "" {
		p.TranscriptPath = update.TranscriptPath
	}
	p.ContextPercent = update.ContextPercent
	p.QuotaRemaining = update.QuotaRemaining
	p.ToolPending = update.ToolPending
	p.CurrentTool = update.CurrentTool
	p.LastHeartbeat = time.Now()

	// 映射状态机
	if update.ToolPending {
		p.Status = StatusAwaitingConfirmation
	} else {
		switch update.AgentState {
		case "thinking":
			p.Status = StatusThinking
		case "working":
			p.Status = StatusWorking
		case "tool_use":
			p.Status = StatusToolUse
		case "idle":
			p.Status = StatusIdle
		case "terminated":
			p.Status = StatusTerminated
		default:
			if p.Status == "" || p.Status == StatusUnknown {
				p.Status = StatusWorking
			}
		}
	}

	eventType := EventProcessUpdated
	if !exists {
		eventType = EventProcessDiscovered
	} else if p.Status == StatusAwaitingConfirmation {
		eventType = EventProcessToolPending
	}

	r.notify(ProcessEvent{
		Type:    eventType,
		Process: p,
	})

	return p
}

// Get 根据 ID 获取进程实体
func (r *Registry) Get(id string) (*AgentProcess, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.processes[id]
	if !ok {
		return nil, false
	}
	// 返回浅拷贝防止并发读写
	copyProc := *p
	return &copyProc, true
}

// List 列出所有已注册的进程实体
func (r *Registry) List() []*AgentProcess {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := make([]*AgentProcess, 0, len(r.processes))
	for _, p := range r.processes {
		copyProc := *p
		res = append(res, &copyProc)
	}
	return res
}

// SetToolPending 设置指定进程的工具审批等待状态
func (r *Registry) SetToolPending(id string, pending bool, currentTool string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, exists := r.processes[id]
	if !exists {
		return
	}
	p.ToolPending = pending
	p.CurrentTool = currentTool
	if pending {
		p.Status = StatusAwaitingConfirmation
	} else if p.Status == StatusAwaitingConfirmation {
		p.Status = StatusWorking
	}

	r.notify(ProcessEvent{
		Type:    EventProcessToolPending,
		Process: p,
	})
}

// SweepExpired 淘汰超出 TTL 范围的心跳失联实例
func (r *Registry) SweepExpired(ttl time.Duration) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	expired := make([]string, 0)
	for id, p := range r.processes {
		if now.Sub(p.LastHeartbeat) > ttl {
			expired = append(expired, id)
			p.Status = StatusTerminated
			r.notify(ProcessEvent{
				Type:    EventProcessTerminated,
				Process: p,
			})
			delete(r.processes, id)
		}
	}
	return expired
}
