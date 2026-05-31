package motor

import "context"

const LayerName = "motor"

// Command 描述 Motor 层要执行的最小动作单元。
type Command struct {
	ID      string                 `json:"id"`
	Action  string                 `json:"action"`
	Input   map[string]interface{} `json:"input,omitempty"`
	Meta    map[string]string      `json:"meta,omitempty"`
	Confirm bool                   `json:"confirm,omitempty"`
}

// Result 是 Motor 层执行后的标准结果。
type Result struct {
	CommandID   string            `json:"command_id"`
	Observation string            `json:"observation"`
	Done        bool              `json:"done"`
	Meta        map[string]string `json:"meta,omitempty"`
}

// Module 定义 Motor 层的最小职责：接收动作命令并完成执行反馈。
type Module interface {
	Name() string
	Act(ctx context.Context, command Command) (Result, error)
}
