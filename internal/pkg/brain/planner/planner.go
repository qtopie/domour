package planner

import (
	"context"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/pkg/brain/diencephalon"
)

// NewPlanner 创建一个基于 LLM 的计划生成节点
func NewPlanner(chatModel diencephalon.Client) *compose.Lambda {
	return compose.InvokableLambda(func(ctx context.Context, input string) (string, error) {
		// 使用 LLM 生成计划
		msg := []*schema.Message{
			schema.SystemMessage("You are a project planner. Break down the user's request into a step-by-step execution plan."),
			schema.UserMessage(input),
		}

		resp, err := chatModel.GenerateText(ctx, msg)
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	})
}
