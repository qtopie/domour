package cerebellum

// TaskGraph 代表小脑编排出的有向无环图(DAG)任务执行流
type TaskGraph struct {
	IntentID string
	Nodes    []*TaskNode
	// 可以继续扩展 DAG 相关的存储结构
}

// TaskNode 代表任务图中具体执行的某个技能或工具动作
type TaskNode struct {
	ID        string
	ToolName  string
	Arguments map[string]interface{}
}

// TaskResult 代表单一任务的执行明细
type TaskResult struct {
	NodeID  string
	Success bool
	Output  interface{}
	Error   error
}

// SensorFeedback 传感器/工具执行给回的异步回调结果
type SensorFeedback struct {
	SourceID string
	Payload  interface{}
}