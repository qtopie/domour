package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

type approvalItem struct {
	decisionCh chan decisionResult
	toolName   string
}

type decisionResult struct {
	decision ToolDecision
	reason   string
}

// ManagedProcess 表示由 Supervisor 直接拉起并托管流式管道的 Agent 子进程
type ManagedProcess struct {
	ID        string
	Provider  AgentProvider
	Cmd       *exec.Cmd
	Stdin     io.WriteCloser
	Stdout    io.ReadCloser
	Stderr    io.ReadCloser
	Cancel    context.CancelFunc
	Workspace string
}

// Controller 负责审批裁决与指令流下发
type Controller struct {
	mu               sync.RWMutex
	pendingApprovals map[string]*approvalItem
	managed          map[string]*ManagedProcess
	registry         *Registry
}

// NewController 创建控制器
func NewController(registry *Registry) *Controller {
	return &Controller{
		pendingApprovals: make(map[string]*approvalItem),
		managed:          make(map[string]*ManagedProcess),
		registry:         registry,
	}
}

// RegisterManagedProcess 手动注册一个受管进程（如外部创建的管道实例或测试桩）
func (c *Controller) RegisterManagedProcess(mp *ManagedProcess) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.managed[mp.ID] = mp
	if c.registry != nil && mp.Cmd != nil && mp.Cmd.Process != nil {
		c.registry.Register(&AgentProcess{
			ID:        mp.ID,
			PID:       mp.Cmd.Process.Pid,
			Workspace: mp.Workspace,
			Status:    StatusWorking,
		})
	}
}

// WaitForDecision 在 PreToolUse 拦截时挂起等待用户或上层系统的审批裁决
func (c *Controller) WaitForDecision(ctx context.Context, processID, toolName string, timeout time.Duration) (ToolDecision, string) {
	ch := make(chan decisionResult, 1)

	c.mu.Lock()
	c.pendingApprovals[processID] = &approvalItem{
		decisionCh: ch,
		toolName:   toolName,
	}
	c.mu.Unlock()

	// 告知 Registry 进入审批等待
	if c.registry != nil {
		c.registry.SetToolPending(processID, true, toolName)
	}

	defer func() {
		c.mu.Lock()
		delete(c.pendingApprovals, processID)
		c.mu.Unlock()
		if c.registry != nil {
			c.registry.SetToolPending(processID, false, toolName)
		}
	}()

	select {
	case <-ctx.Done():
		return DecisionDeny, "Request context cancelled"
	case <-time.After(timeout):
		return DecisionDeny, "Approval timeout"
	case res := <-ch:
		return res.decision, res.reason
	}
}

// SubmitToolDecision 外部提交审批决策，唤醒挂起的 Hook
func (c *Controller) SubmitToolDecision(processID string, decision ToolDecision, reason string) error {
	c.mu.Lock()
	item, ok := c.pendingApprovals[processID]
	c.mu.Unlock()

	if !ok {
		return fmt.Errorf("no pending tool approval found for process %s", processID)
	}

	select {
	case item.decisionCh <- decisionResult{decision: decision, reason: reason}:
		return nil
	default:
		return fmt.Errorf("decision already submitted for process %s", processID)
	}
}

// LaunchHeadless 启动一个 Headless 流式受管进程
func (c *Controller) LaunchHeadless(ctx context.Context, provider AgentProvider, command string, opts LaunchOptions) (*ManagedProcess, error) {
	args, err := provider.BuildHeadlessArgs(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to build headless args: %w", err)
	}

	if command == "" {
		command = "agy"
	}

	subCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(subCtx, command, args...)
	if opts.Workspace != "" {
		cmd.Dir = opts.Workspace
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start headless process: %w", err)
	}

	id := opts.SessionID
	if id == "" {
		id = fmt.Sprintf("proc-%d", cmd.Process.Pid)
	}

	mp := &ManagedProcess{
		ID:        id,
		Provider:  provider,
		Cmd:       cmd,
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stderr,
		Cancel:    cancel,
		Workspace: opts.Workspace,
	}

	c.mu.Lock()
	c.managed[id] = mp
	c.mu.Unlock()

	if c.registry != nil {
		c.registry.Register(&AgentProcess{
			ID:        id,
			PID:       cmd.Process.Pid,
			Provider:  provider.Name(),
			Workspace: opts.Workspace,
			Status:    StatusWorking,
		})
	}

	return mp, nil
}

// SendInstruction 向指定实例发送指令
func (c *Controller) SendInstruction(ctx context.Context, processID string, prompt string) error {
	c.mu.RLock()
	mp, exists := c.managed[processID]
	c.mu.RUnlock()

	if exists && mp.Stdin != nil {
		var data []byte
		var err error
		if mp.Provider != nil {
			data, err = mp.Provider.FormatInstruction(prompt)
		} else {
			payload := map[string]interface{}{
				"role":    "user",
				"content": prompt,
			}
			data, err = json.Marshal(payload)
			data = append(data, '\n')
		}
		if err != nil {
			return err
		}
		_, err = mp.Stdin.Write(data)
		return err
	}

	// 如果不是内部托管的子进程，则向已存在外部会话发起续接命令
	if c.registry != nil {
		proc, ok := c.registry.Get(processID)
		if !ok {
			return fmt.Errorf("process %s not found in registry", processID)
		}

		// 创建一次性执行以继续
		cmd := exec.CommandContext(ctx, "agy", "-c", proc.ID, "-p", prompt, "--add-dir", proc.Workspace)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to resume session %s: %w", proc.ID, err)
		}
		return nil
	}

	return fmt.Errorf("unable to dispatch instruction for process %s", processID)
}

// StopProcess 停止受管子进程
func (c *Controller) StopProcess(processID string) error {
	c.mu.Lock()
	mp, exists := c.managed[processID]
	delete(c.managed, processID)
	c.mu.Unlock()

	if !exists {
		return nil
	}

	mp.Cancel()
	if mp.Cmd.Process != nil {
		_ = mp.Cmd.Process.Kill()
	}
	return nil
}
