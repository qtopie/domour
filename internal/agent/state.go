package agent

// TaskStatus 定义任务生命周期
type TaskStatus string

const (
	StatusPending   TaskStatus = "pending"
	StatusRunning   TaskStatus = "running"
	StatusCompleted TaskStatus = "completed"
	StatusFailed    TaskStatus = "failed"
)

// TaskStep 原子任务定义
type TaskStep struct {
	ID          string                 `json:"id"`
	Action      string                 `json:"action"` // 对应 Worker 的 Key
	Input       map[string]interface{} `json:"input"`
	Status      TaskStatus             `json:"status"`
	Observation string                 `json:"observation"` // 执行结果反馈
}

// State 整个大脑的上下文看板
type State struct {
	SessionID     string
	GlobalGoal    string
	Complexity    int // 1-10 评分
	Steps         []*TaskStep
	CurrentStepID string
	UserFeedback  string // 存储执行中用户的新指令
}

const (
	ComplexitySimple  = 1 // Direct
	ComplexityGeneral = 5 // ReAct
	ComplexityComplex = 9 // Planner/Worker
)
