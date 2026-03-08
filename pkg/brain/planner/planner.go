package planner

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// NewPlanner 创建一个基于 LLM 的计划生成节点
func NewPlanner(chatModel model.ChatModel) *compose.Lambda {
	return compose.InvokableLambda(func(ctx context.Context, input string) (string, error) {
		// 使用 LLM 生成计划
		msg := []*schema.Message{
			schema.SystemMessage("You are a project planner. Break down the user's request into a step-by-step execution plan."),
			schema.UserMessage(input),
		}

		resp, err := chatModel.Generate(ctx, msg)
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	})
}
