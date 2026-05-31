package react

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/agent/diencephalon"
)

// CalculatorTool definition
var CalculatorTool = &schema.ToolInfo{
	Name: "calculator",
	Desc: "Basic calculator. Input: expression string (e.g. '2 + 2')",
}

// NewReActGraph creates a ReAct agent graph
func NewReActGraph(m diencephalon.Client) (compose.Runnable[[]*schema.Message, []*schema.Message], error) {
	// Bind tool to model
	if err := m.BindTools([]*schema.ToolInfo{CalculatorTool}); err != nil {
		return nil, fmt.Errorf("failed to bind tools: %w", err)
	}

	// Graph State: []*schema.Message
	g := compose.NewGraph[[]*schema.Message, []*schema.Message]()

	// 1. Think Node: Calls LLM
	_ = g.AddLambdaNode("think", compose.InvokableLambda(func(ctx context.Context, input []*schema.Message) ([]*schema.Message, error) {
		resp, err := m.GenerateMessage(ctx, input)
		if err != nil {
			return nil, err
		}
		// Append response to history
		// Note: 'input' is slice, append returns new slice.
		// We return the NEW slice which becomes the state for the next node.
		return append(input, resp), nil
	}))

	// 2. Act Node: Executes Tool
	_ = g.AddLambdaNode("act", compose.InvokableLambda(func(ctx context.Context, input []*schema.Message) ([]*schema.Message, error) {
		last := input[len(input)-1]

		var newMsgs = []*schema.Message{}
		// Copy input to avoid modifying shared state if any (though here it's passed by value/copy usually)
		newMsgs = append(newMsgs, input...)

		for _, tc := range last.ToolCalls {
			if tc.Function.Name == "calculator" {
				var args map[string]interface{}
				var res string
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					res = fmt.Sprintf("Error parsing args: %v", err)
				} else {
					expr, _ := args["expression"].(string)
					log.Printf("[ReAct] Calculator Tool Called: %s", expr)

					// Mock logic
					if strings.Contains(expr, "2") && strings.Contains(expr, "+") {
						res = "4"
					} else {
						res = "Calculated: " + expr
					}
				}

				toolMsg := &schema.Message{
					Role:       schema.Tool,
					Content:    res,
					ToolCallID: tc.ID,
				}
				newMsgs = append(newMsgs, toolMsg)
			}
		}
		return newMsgs, nil
	}))

	// Edges
	_ = g.AddEdge(compose.START, "think")

	// Router
	branch := compose.NewGraphBranch(func(ctx context.Context, input []*schema.Message) (string, error) {
		last := input[len(input)-1]
		if len(last.ToolCalls) > 0 {
			return "act", nil
		}
		return compose.END, nil
	}, map[string]bool{"act": true, compose.END: true})

	_ = g.AddBranch("think", branch)

	// Loop
	_ = g.AddEdge("act", "think")

	return g.Compile(context.Background())
}
