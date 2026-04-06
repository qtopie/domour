package brain

import "context"

// Brain 控制中枢，暴露给其他系统的交互入口
type Brain interface {
	// Think 接收外部事件和观察结果，基于记忆和规则/LLM进行推理，给出高层意图
	Think(ctx context.Context, observation Observation) (Intent, error)

	// Memorize 将信息存入记忆中观（短期或长期）
	Memorize(ctx context.Context, info MemoryPayload) error

	// Recall 从长期或短期缓存检索记忆库
	Recall(ctx context.Context, query string) ([]MemoryPayload, error)
}

// Consciousness 主控意识体：一直运行的守护进程，不断审视系统自身状态
type Consciousness interface {
	// Start 启动意识的自主巡检循环
	Start(ctx context.Context) error

	// Stop 停止巡检
	Stop()

	// InspectSelf 进行一次自我状态的反馈审查
	InspectSelf() SystemHealthStatus
}
