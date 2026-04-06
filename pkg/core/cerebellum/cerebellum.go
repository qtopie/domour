package cerebellum

import (
	"context"

	"github.com/qtopie/domour/pkg/core/brain"
)

// Cerebellum 小脑，负责逻辑层面的任务流执行与反馈感知
type Cerebellum interface {
	// Orchestrate 大脑给一个意图，小脑负责编排出一组可以执行的任务流程(执行图)
	Orchestrate(ctx context.Context, intent brain.Intent) (*TaskGraph, error)

	// ExecuteTask 处理具体节点的工具逻辑调用，并捕获返回值
	ExecuteTask(ctx context.Context, task TaskNode) (TaskResult, error)

	// HandleFeedback 接收动作物理结果或传感器回调，汇聚后向上流转回大脑
	HandleFeedback(ctx context.Context, feedback SensorFeedback) error
}
