package stem

import "context"

const LayerName = "stem"

// Signal 是进入系统的统一信号封装，可来自 chat/copilot/autopilot 等入口。
type Signal struct {
	SessionID   string            `json:"session_id"`
	Entry       string            `json:"entry"`
	Workspace   string            `json:"workspace,omitempty"`
	Message     string            `json:"message,omitempty"`
	Constraints []string          `json:"constraints,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
}

// Decision 是 Stem 层对输入信号的最小处理结果。
type Decision struct {
	Allowed bool              `json:"allowed"`
	Route   string            `json:"route"`
	Reason  string            `json:"reason,omitempty"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// Module 定义 Stem 层的最小职责：接收入站信号、做守卫与初始路由。
type Module interface {
	Name() string
	Process(ctx context.Context, signal Signal) (Decision, error)
}
