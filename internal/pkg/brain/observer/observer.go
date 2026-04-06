package observer

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/pkg/brain/diencephalon"
)

// NewObserver 创建一个基于 LLM 的复杂度评估节点
func NewObserver(chatModel diencephalon.Client) *compose.Lambda {
	return compose.InvokableLambda(func(ctx context.Context, input string) (string, error) {
		// 使用 LLM 评估复杂度
		// 这里我们简化 prompt，实际可以使用 prompt template
		msg := []*schema.Message{
			schema.SystemMessage("You are a complexity analyzer. Determine if the user's task is 'simple', 'general', or 'complex'. 'simple' for direct answers. 'general' for multi-step tasks solvable with known tools (ReAct). 'complex' for tasks requiring planning and decomposition. Return only 'simple', 'general', or 'complex'."),
			schema.UserMessage(input),
		}

		resp, err := chatModel.GenerateText(ctx, msg)
		if err != nil {
			return "", err
		}

		result := strings.ToLower(strings.TrimSpace(resp.Content))
		if strings.Contains(result, "complex") {
			return "complex", nil
		}
		if strings.Contains(result, "general") {
			return "general", nil
		}
		return "simple", nil
	})
}
