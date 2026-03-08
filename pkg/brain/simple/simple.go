package simple

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func NewSimpleHandler(m model.ChatModel) *compose.Lambda {
	return compose.InvokableLambda(func(ctx context.Context, input string) (string, error) {
		msg := []*schema.Message{
			schema.UserMessage(input),
		}
		resp, err := m.Generate(ctx, msg)
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	})
}
