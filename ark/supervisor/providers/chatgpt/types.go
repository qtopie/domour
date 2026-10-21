package chatgpt

import "time"

// ThreadRecord 对应 ~/.codex/state_5.sqlite 中 threads 表的记录
type ThreadRecord struct {
	ID               string    `json:"id"`
	RolloutPath      string    `json:"rollout_path"`
	CreatedAt        int64     `json:"created_at"`
	UpdatedAt        int64     `json:"updated_at"`
	Source           string    `json:"source"`
	ModelProvider    string    `json:"model_provider"`
	CWD              string    `json:"cwd"`
	Title            string    `json:"title"`
	SandboxPolicy    string    `json:"sandbox_policy"`
	ApprovalMode     string    `json:"approval_mode"`
	TokensUsed       int       `json:"tokens_used"`
	Model            string    `json:"model"`
	FirstUserMessage string    `json:"first_user_message"`
	LastUpdated      time.Time `json:"last_updated"`
}

// JSONRPCRequest 标准 JSON-RPC 2.0 请求
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse 标准 JSON-RPC 2.0 响应
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError 标准 JSON-RPC 2.0 错误
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// CodexEvent 表示 codex exec --json 输出的单行事件
type CodexEvent struct {
	Type        string                 `json:"type,omitempty"`
	ThreadID    string                 `json:"thread_id,omitempty"`
	Method      string                 `json:"method,omitempty"`
	Role        string                 `json:"role,omitempty"`
	Content     string                 `json:"content,omitempty"`
	Message     string                 `json:"message,omitempty"`
	Name        string                 `json:"name,omitempty"`
	Arguments   map[string]interface{} `json:"arguments,omitempty"`
	Output      string                 `json:"output,omitempty"`
	TurnID      string                 `json:"turn_id,omitempty"`
	TokensUsed  int                    `json:"tokens_used,omitempty"`
	Params      map[string]interface{} `json:"params,omitempty"`
	Item        map[string]interface{} `json:"item,omitempty"`
}
