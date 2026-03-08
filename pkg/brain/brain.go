package brain

import (
	"context"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// Brain 抽象接口
type Brain interface {
	// Execute 接收输入，返回最终结果
	Execute(ctx context.Context, input string) (string, error)
	// Visualize returns the D2 representation of the execution graph
	Visualize() string
}

type adaptiveBrain struct {
	graph compose.Runnable[string, string]
}

func (b *adaptiveBrain) Visualize() string {
	return `
direction: right
start -> observer
observer -> simple_handler: simple
observer -> react_agent: general
observer -> planner: complex
planner -> worker
simple_handler -> end
react_agent -> end
worker -> end
`
}

func (b *adaptiveBrain) Execute(ctx context.Context, input string) (string, error) {
	return b.graph.Invoke(ctx, input)
}

// NewBrain 构建自适应编排图
func NewBrain(
	observer *compose.Lambda,
	planner *compose.Lambda,
	worker *compose.Lambda,
	reactAgent compose.Runnable[[]*schema.Message, []*schema.Message],
	simpleHandler *compose.Lambda,
) (Brain, error) {
	g := compose.NewGraph[string, string]()

	// 1. 添加节点
	_ = g.AddLambdaNode("observer", observer)
	_ = g.AddLambdaNode("planner", planner)
	_ = g.AddLambdaNode("worker", worker)
	_ = g.AddLambdaNode("simple_handler", simpleHandler)

	// Adapter for ReAct: string -> []*Message -> string
	_ = g.AddLambdaNode("react_agent", compose.InvokableLambda(func(ctx context.Context, input string) (string, error) {
		msgs := []*schema.Message{schema.UserMessage(input)}
		out, err := reactAgent.Invoke(ctx, msgs)
		if err != nil {
			return "", err
		}
		if len(out) == 0 {
			return "", nil
		}
		return out[len(out)-1].Content, nil
	}))

	// 2. Start -> Observer
	_ = g.AddEdge(compose.START, "observer")

	// 3. 定义自适应路由 (Branch)
	_ = g.AddBranch("observer", compose.NewGraphBranch(func(ctx context.Context, in string) (string, error) {
		switch in {
		case "complex":
			return "planner", nil
		case "general":
			return "react_agent", nil
		default:
			return "simple_handler", nil
		}
	}, map[string]bool{
		"planner":       true,
		"react_agent":   true,
		"simple_handler": true,
	}))

	// 4. Edges for paths
	_ = g.AddEdge("planner", "worker")
	_ = g.AddEdge("worker", compose.END)
	_ = g.AddEdge("react_agent", compose.END)
	_ = g.AddEdge("simple_handler", compose.END)

	runnable, err := g.Compile(context.Background())
	if err != nil {
		return nil, err
	}
	return &adaptiveBrain{graph: runnable}, nil
}
