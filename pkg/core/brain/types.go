package brain

// Observation 表示大脑接收到的外界输入或刺激
type Observation struct {
	Source string
	Data   interface{}
}

// Intent 表示大脑经过思考后下发的高层意图
type Intent struct {
	GoalID      string
	Description string
	Payload     map[string]interface{}
}

// MemoryPayload 记忆片段
type MemoryPayload struct {
	ID        string
	Content   string
	Timestamp int64
	Tags      []string
}

// SystemHealthStatus 意识体巡检出来的系统自我健康状况
type SystemHealthStatus struct {
	IsHealthy    bool
	MemoryUsage  float64
	ActiveMinds  int
	LastSelfDiag string
}
