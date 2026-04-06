package brainstem

// NodeInfo 表示底层 cosmos-star 集群中的边缘节点信息
type NodeInfo struct {
	NodeID   string
	Role     string
	IsOnline bool
	Tags     map[string]string
}

// AgentStatus 表示上方 domour (大脑+小脑) 的健康信息
type AgentStatus struct {
	Health        bool
	ActiveIntents int
	MemoryUsage   float64
}

// JobPayload 小脑投递给底层执行的物理任务包
type JobPayload struct {
	TaskNodeID string
	ToolName   string
	Arguments  []byte
}

// JobResult 底层边缘节点执行完传回的结果
type JobResult struct {
	JobID   string
	Success bool
	Output  []byte
	Error   string
}