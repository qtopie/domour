package chatgpt

import (
	"encoding/json"
	"fmt"
)

// NewJSONRPCRequest 构造标准 JSON-RPC 2.0 请求
func NewJSONRPCRequest(id interface{}, method string, params interface{}) *JSONRPCRequest {
	return &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
}

// FormatApprovalDecision 将通用审批决策转换为 JSON-RPC 响应或审批结构
func FormatApprovalDecision(decision string, reason string) ([]byte, error) {
	resp := map[string]interface{}{
		"decision": decision,
	}
	if reason != "" {
		resp["reason"] = reason
	}
	return json.Marshal(resp)
}

// ParseJSONRPCMessage 解析接收到的 JSON-RPC 原始字节
func ParseJSONRPCMessage(data []byte) (*JSONRPCRequest, *JSONRPCResponse, error) {
	var probe struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method,omitempty"`
		ID      interface{}     `json:"id,omitempty"`
		Error   *JSONRPCError   `json:"error,omitempty"`
		Result  json.RawMessage `json:"result,omitempty"`
	}

	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, nil, fmt.Errorf("failed to probe JSON-RPC message: %w", err)
	}

	if probe.Method != "" {
		var req JSONRPCRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, nil, err
		}
		return &req, nil, nil
	}

	var resp JSONRPCResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, nil, err
	}
	return nil, &resp, nil
}
