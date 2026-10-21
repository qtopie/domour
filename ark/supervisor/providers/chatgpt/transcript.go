package chatgpt

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/qtopie/domour/ark/supervisor"
)

// ParseTranscriptLine 将 codex exec --json 输出的单行反序列化为统一的 TranscriptRecord
func ParseTranscriptLine(line []byte) (*supervisor.TranscriptRecord, error) {
	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" {
		return nil, fmt.Errorf("empty line")
	}

	var ev CodexEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		// 尝试作为通用格式解析
		var generic map[string]interface{}
		if err2 := json.Unmarshal(line, &generic); err2 == nil {
			rec := &supervisor.TranscriptRecord{
				Type:      "event",
				Timestamp: time.Now(),
				RawLine:   line,
				Metadata:  generic,
			}
			if role, ok := generic["role"].(string); ok {
				rec.Role = role
			}
			if content, ok := generic["content"].(string); ok {
				rec.Content = content
			}
			return rec, nil
		}
		return nil, fmt.Errorf("failed to parse transcript line: %w", err)
	}

	rec := &supervisor.TranscriptRecord{
		RawLine:   line,
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	// 1. 会话建立
	if ev.Type == "thread.started" || ev.Type == "thread_started" {
		rec.Type = "thread_started"
		rec.Role = "system"
		rec.Content = ev.ThreadID
		if ev.ThreadID != "" {
			rec.Metadata["thread_id"] = ev.ThreadID
		}
		return rec, nil
	}

	// 2. 轮次开始
	if ev.Type == "turn.started" || ev.Type == "turn_started" {
		rec.Type = "turn_started"
		rec.Role = "system"
		return rec, nil
	}

	// 3. 错误事件
	if ev.Type == "error" {
		rec.Type = "error"
		rec.Role = "system"
		rec.Content = ev.Message
		return rec, nil
	}

	// 4. 普通助手消息
	if ev.Type == "agent_message" || ev.Type == "message" || ev.Role == "assistant" {
		rec.Type = "assistant_response"
		rec.Role = "assistant"
		rec.Content = ev.Content
		return rec, nil
	}

	// 5. item.completed 或 item.delta
	if ev.Type == "item.completed" || ev.Type == "item.delta" {
		if itemType, ok := ev.Item["type"].(string); ok {
			if itemType == "agent_message" {
				rec.Type = "assistant_response"
				rec.Role = "assistant"
				if text, ok := ev.Item["text"].(string); ok {
					rec.Content = text
				}
				return rec, nil
			}
			if itemType == "error" {
				rec.Type = "error"
				rec.Role = "system"
				if msg, ok := ev.Item["message"].(string); ok {
					rec.Content = msg
				}
				return rec, nil
			}
		}
	}

	// 6. 工具调用事件
	if ev.Type == "tool_call" || ev.Type == "call" {
		rec.Type = "tool_use"
		rec.Role = "assistant"
		toolName := ev.Name
		if toolName == "" {
			if tn, ok := ev.Arguments["tool"].(string); ok {
				toolName = tn
			}
		}
		rec.ToolCalls = []supervisor.ToolCallInfo{
			{
				ToolName:  toolName,
				Arguments: ev.Arguments,
			},
		}
		return rec, nil
	}

	// 7. 工具执行结果
	if ev.Type == "tool_result" || ev.Role == "tool" {
		rec.Type = "tool_result"
		rec.Role = "tool"
		rec.Content = ev.Output
		if rec.Content == "" {
			rec.Content = ev.Content
		}
		return rec, nil
	}

	// 8. 轮次完成
	if ev.Type == "turn.completed" || ev.Type == "turn_completed" {
		rec.Type = "turn_completed"
		rec.Role = "system"
		rec.Metadata["tokens_used"] = ev.TokensUsed
		return rec, nil
	}

	// 5. 用户输入
	if ev.Type == "user_input" || ev.Role == "user" {
		rec.Type = "user_input"
		rec.Role = "user"
		rec.Content = ev.Content
		return rec, nil
	}

	// 6. JSON-RPC 增量通知 item/agentMessage/delta
	if strings.Contains(ev.Method, "agentMessage") {
		rec.Type = "assistant_delta"
		rec.Role = "assistant"
		if delta, ok := ev.Params["content"].(string); ok {
			rec.Content = delta
		}
		return rec, nil
	}

	// 默认回退
	rec.Type = ev.Type
	if rec.Type == "" {
		rec.Type = "event"
	}
	rec.Role = ev.Role
	rec.Content = ev.Content
	return rec, nil
}

// ReadTurnsFromRollout 读取 rollout 文件中最近的 turns
func ReadTurnsFromRollout(path string, maxTurns int) ([]*supervisor.TranscriptRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open rollout file %s: %w", path, err)
	}
	defer file.Close()

	var records []*supervisor.TranscriptRecord
	scanner := bufio.NewScanner(file)
	step := 1

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}

		var raw map[string]interface{}
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}

		rec := &supervisor.TranscriptRecord{
			StepIndex: step,
			Timestamp: time.Now(),
			RawLine:   line,
		}
		step++

		// 支持 Codex 官方 rollout 嵌套结构 (type + payload)
		lineType, _ := raw["type"].(string)
		if lineType == "session_meta" || lineType == "turn_context" {
			// 过滤纯内部配置与上下文元数据，避免污染会话回放
			continue
		}

		payload, hasPayload := raw["payload"].(map[string]interface{})
		target := raw
		if hasPayload {
			target = payload
		}

		if r, ok := target["role"].(string); ok {
			rec.Role = r
		}
		if c, ok := target["content"].(string); ok {
			rec.Content = c
		} else if contentList, ok := target["content"].([]interface{}); ok {
			var sb strings.Builder
			for _, elem := range contentList {
				if elemMap, ok := elem.(map[string]interface{}); ok {
					if txt, ok := elemMap["text"].(string); ok {
						sb.WriteString(txt)
					}
				}
			}
			rec.Content = sb.String()
		} else if msg, ok := target["message"].(string); ok {
			rec.Content = msg
		}

		// 推断角色与类型
		if rec.Role == "" {
			if targetType, ok := target["type"].(string); ok {
				if strings.Contains(targetType, "user") {
					rec.Role = "user"
					rec.Type = "user_input"
				} else if strings.Contains(targetType, "agent") || strings.Contains(targetType, "assistant") {
					rec.Role = "assistant"
					rec.Type = "assistant_response"
				}
			}
		}

		if rec.Content == "" && len(rec.ToolCalls) == 0 {
			// 没有可读内容的事件跳过
			continue
		}

		// 解析 tool_calls
		if callsRaw, ok := raw["tool_calls"].([]interface{}); ok {
			for _, item := range callsRaw {
				if m, ok := item.(map[string]interface{}); ok {
					call := supervisor.ToolCallInfo{}
					if name, ok := m["name"].(string); ok {
						call.ToolName = name
					}
					if args, ok := m["arguments"].(map[string]interface{}); ok {
						call.Arguments = args
					}
					rec.ToolCalls = append(rec.ToolCalls, call)
				}
			}
		}

		records = append(records, rec)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading rollout file: %w", err)
	}

	if maxTurns > 0 && len(records) > maxTurns {
		records = records[len(records)-maxTurns:]
	}

	return records, nil
}
