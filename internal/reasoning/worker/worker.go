package worker

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/compose"
)

// NewDispatcher 创建一个简单的执行分发节点
func NewDispatcher() *compose.Lambda {
	return compose.InvokableLambda(func(ctx context.Context, input string) (string, error) {
		return fmt.Sprintf("Executed plan: %s", input), nil
	})
}
