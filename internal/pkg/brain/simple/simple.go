package simple

import (
	"context"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/pkg/brain/diencephalon"
)

func NewSimpleHandler(m diencephalon.Client) *compose.Lambda {
	return compose.InvokableLambda(func(ctx context.Context, input string) (string, error) {
		msg := []*schema.Message{
			schema.UserMessage(input),
		}
		resp, err := m.GenerateText(ctx, msg)
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	})
}
