package antigravity

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// AgentAPINewConversationResult 表示创建新会话的返回结果
type AgentAPINewConversationResult struct {
	ConversationID string `json:"conversationId"`
	Prompt         string `json:"prompt"`
}

// AgentAPISendMessageResult 表示发送消息的返回结果
type AgentAPISendMessageResult struct {
	RecipientID string `json:"recipientId"`
	Content     string `json:"content"`
}

// AgentAPIMetadataResult 表示会话元数据返回结果
type AgentAPIMetadataResult struct {
	RootConversationID string `json:"rootConversationId"`
	CreatedAt          string `json:"createdAt"`
	Workspaces         []struct {
		WorkspaceFolderAbsoluteUri string `json:"workspaceFolderAbsoluteUri"`
	} `json:"workspaces"`
}

type agentAPIResponseWrapper struct {
	Error    string `json:"error,omitempty"`
	Response *struct {
		NewConversation      *AgentAPINewConversationResult `json:"newConversation,omitempty"`
		SendMessage          *AgentAPISendMessageResult     `json:"sendMessage,omitempty"`
		ConversationMetadata *struct {
			Metadata *AgentAPIMetadataResult `json:"metadata,omitempty"`
		} `json:"conversationMetadata,omitempty"`
	} `json:"response,omitempty"`
}

// AgentAPIClient 定义与 Antigravity 语言服务 AgentAPI 交互的接口
type AgentAPIClient interface {
	IsAvailable() bool
	NewConversation(ctx context.Context, prompt string, model string, title string) (*AgentAPINewConversationResult, error)
	SendMessage(ctx context.Context, recipientID string, content string, title string) (*AgentAPISendMessageResult, error)
	GetMetadata(ctx context.Context, conversationID string) (*AgentAPIMetadataResult, error)
}

type defaultAgentAPIClient struct {
	exePath      string
	isSubcommand bool
	available    bool
}

// NewAgentAPIClient 创建默认的 AgentAPI 客户端实例
func NewAgentAPIClient() AgentAPIClient {
	exe, isSubcmd, ok := FindAgentAPI()
	return &defaultAgentAPIClient{
		exePath:      exe,
		isSubcommand: isSubcmd,
		available:    ok,
	}
}

func (c *defaultAgentAPIClient) IsAvailable() bool {
	return c.available && c.exePath != ""
}

func (c *defaultAgentAPIClient) runCommand(ctx context.Context, subcmd string, args []string) ([]byte, error) {
	if !c.IsAvailable() {
		return nil, fmt.Errorf("agentapi executable not found in this environment")
	}

	var cmdArgs []string
	if c.isSubcommand {
		cmdArgs = append([]string{"agentapi", subcmd}, args...)
	} else {
		cmdArgs = append([]string{subcmd}, args...)
	}

	cmd := exec.CommandContext(ctx, c.exePath, cmdArgs...)
	out, err := cmd.Output()
	if err != nil {
		if len(out) > 0 {
			var errWrap agentAPIResponseWrapper
			if jErr := json.Unmarshal(out, &errWrap); jErr == nil && errWrap.Error != "" {
				return nil, fmt.Errorf("agentapi error: %s", errWrap.Error)
			}
			return nil, fmt.Errorf("agentapi execution failed: %s (err: %w)", string(out), err)
		}
		return nil, fmt.Errorf("agentapi execution failed: %w", err)
	}

	return out, nil
}

// NewConversation 在 Antigravity 中拉起全新 Agent 会话
func (c *defaultAgentAPIClient) NewConversation(ctx context.Context, prompt string, model string, title string) (*AgentAPINewConversationResult, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("prompt cannot be empty")
	}

	var args []string
	if model != "" {
		args = append(args, "--model="+model)
	}
	if title != "" {
		args = append(args, "--title="+title)
	}
	args = append(args, prompt)

	out, err := c.runCommand(ctx, "new-conversation", args)
	if err != nil {
		return nil, err
	}

	var wrap agentAPIResponseWrapper
	if err := json.Unmarshal(out, &wrap); err != nil {
		return nil, fmt.Errorf("failed to parse new-conversation response: %w", err)
	}
	if wrap.Error != "" {
		return nil, fmt.Errorf("agentapi error: %s", wrap.Error)
	}
	if wrap.Response == nil || wrap.Response.NewConversation == nil {
		return nil, fmt.Errorf("invalid response structure: missing newConversation")
	}

	return wrap.Response.NewConversation, nil
}

// SendMessage 向 Antigravity 会话追加或注入新消息
func (c *defaultAgentAPIClient) SendMessage(ctx context.Context, recipientID string, content string, title string) (*AgentAPISendMessageResult, error) {
	if recipientID == "" {
		return nil, fmt.Errorf("recipientID cannot be empty")
	}
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("content cannot be empty")
	}

	var args []string
	if title != "" {
		args = append(args, "--title="+title)
	}
	args = append(args, recipientID, content)

	out, err := c.runCommand(ctx, "send-message", args)
	if err != nil {
		return nil, err
	}

	var wrap agentAPIResponseWrapper
	if err := json.Unmarshal(out, &wrap); err != nil {
		return nil, fmt.Errorf("failed to parse send-message response: %w", err)
	}
	if wrap.Error != "" {
		return nil, fmt.Errorf("agentapi error: %s", wrap.Error)
	}
	if wrap.Response == nil || wrap.Response.SendMessage == nil {
		return nil, fmt.Errorf("invalid response structure: missing sendMessage")
	}

	return wrap.Response.SendMessage, nil
}

// GetMetadata 调阅特定会话的元数据
func (c *defaultAgentAPIClient) GetMetadata(ctx context.Context, conversationID string) (*AgentAPIMetadataResult, error) {
	if conversationID == "" {
		return nil, fmt.Errorf("conversationID cannot be empty")
	}

	out, err := c.runCommand(ctx, "get-conversation-metadata", []string{conversationID})
	if err != nil {
		return nil, err
	}

	var wrap agentAPIResponseWrapper
	if err := json.Unmarshal(out, &wrap); err != nil {
		return nil, fmt.Errorf("failed to parse get-conversation-metadata response: %w", err)
	}
	if wrap.Error != "" {
		return nil, fmt.Errorf("agentapi error: %s", wrap.Error)
	}
	if wrap.Response == nil || wrap.Response.ConversationMetadata == nil || wrap.Response.ConversationMetadata.Metadata == nil {
		return nil, fmt.Errorf("invalid response structure: missing conversationMetadata")
	}

	return wrap.Response.ConversationMetadata.Metadata, nil
}
