package agent

import "context"

const LayerName = "brain"

// Thought 是 Brain 输出给下游执行层的最小决策结果。
type Thought struct {
	Intent string            `json:"intent"`
	Route  string            `json:"route"`
	Plan   []string          `json:"plan,omitempty"`
	Risk   string            `json:"risk,omitempty"`
	Meta   map[string]string `json:"meta,omitempty"`
}

// Module 定义 Brain 层的最小职责：理解目标、形成计划、给出路由决策。
type Module interface {
	Name() string
	Think(ctx context.Context, state State) (Thought, error)
}
