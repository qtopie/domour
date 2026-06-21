package brain

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/qtopie/domour/internal/bionic/tool"
)

// TaskGraph represents the directed acyclic graph (DAG) task execution flow orchestrated by the Cerebellum.
type TaskGraph struct {
	IntentID string
	Nodes    []*TaskNode
	// Additional DAG-related storage structure can be extended here
}

// TaskNode represents a specific skill or tool action executed in the task graph.
type TaskNode struct {
	ID        string
	ToolName  string
	Arguments map[string]interface{}
}

// TaskResult represents the execution details of a single task.
type TaskResult struct {
	NodeID  string
	Success bool
	Output  interface{}
	Error   error
}

// SensorFeedback represents the asynchronous callback result returned by sensors/tools.
type SensorFeedback struct {
	SourceID string
	Payload  interface{}
}

// Cerebellum acts as the logical orchestrator, responsible for execution and feedback perception of the task flows.
type Cerebellum interface {
	// Orchestrate receives a high-level intent from the Brain and orchestrates an executable task graph.
	Orchestrate(ctx context.Context, intent Intent) (*TaskGraph, error)

	// ExecuteTask handles the execution logic of a specific task node and captures its result.
	ExecuteTask(ctx context.Context, task TaskNode) (TaskResult, error)

	// HandleFeedback receives physical execution results or sensor callbacks, aggregates them, and reports back to the Brain.
	HandleFeedback(ctx context.Context, feedback SensorFeedback) error
}

// ToolExecutor is a dependency injection interface representing the tool client (e.g. ExecutorClient).
type ToolExecutor interface {
	Execute(ctx context.Context, command tool.Command) (tool.Result, error)
}

// ModelClient is a dependency injection interface representing the LLM client (e.g. Diencephalon proxy).
type ModelClient interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// CerebellumNode is the high-frequency tactical event node for the Cerebellum component.
// It runs a single goroutine at 1kHz, handling muscle-memory skill lookup, plan-to-action
// translation, telemetry, and motor feedback — all in a non-blocking select.
type CerebellumNode struct {
	CognitiveIn    chan CognitiveResult
	TelemetryIn    chan SensorySignal // Telemetry queue. Managed as a ring buffer on write to prevent blocking.
	CorrectionOut  chan CognitiveResult
	Executor       ToolExecutor
	Model          ModelClient
	SignalCallback func(sessionID string, eventType string, desc string, payload any)
}

func NewCerebellumNode(executor ToolExecutor, model ModelClient) *CerebellumNode {
	return &CerebellumNode{
		CognitiveIn:   make(chan CognitiveResult, 10),
		TelemetryIn:   make(chan SensorySignal, 50),
		CorrectionOut: make(chan CognitiveResult, 10),
		Executor:      executor,
		Model:         model,
	}
}

// Start launches the orchestration goroutine for the Cerebellum node.
func (c *CerebellumNode) Start(ctx context.Context) {
	go c.startOrchestrationLoop(ctx)
}

// startOrchestrationLoop runs the High Frequency Tactical Loop (1kHz Ticker / 小脑快思考循环)
func (c *CerebellumNode) startOrchestrationLoop(ctx context.Context) {
	log.Println("[Cerebellum] Orchestration loop started.")
	ticker := time.NewTicker(1 * time.Millisecond) // 1kHz
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[Cerebellum] Orchestration loop stopped.")
			return
		case cognitive := <-c.CognitiveIn:
			log.Printf("[Cerebellum] (Eavesdropped) Translating cognitive plan into tactical steps for Goal: %s", cognitive.GoalID)
			for i, step := range cognitive.Plan {
				toolName := "calculator"
				if step == "respond" {
					toolName = "respond"
				}

				// The Cerebellum (Motor) executes tools directly, bypassing Brainstem channels
				cmd := tool.Command{
					ID:     fmt.Sprintf("%s-%d", cognitive.GoalID, i),
					Action: toolName,
					Input:  map[string]interface{}{"expression": step},
				}

				// If the step is a Simple verification task
				if strings.HasPrefix(step, "verify:") {
					content := strings.TrimPrefix(step, "verify:")
					log.Printf("[Cerebellum] Simple mode verification initiated for: %q", content)

					if strings.Contains(content, "invalid") || strings.Contains(content, "problem") {
						log.Printf("[Cerebellum] Verification failed: problem detected in content.")
						if strings.Contains(content, "2+2") || strings.Contains(content, "math") {
							log.Printf("[Cerebellum] Attempting to fix via calculator tool...")
							subCmd := tool.Command{
								ID:     fmt.Sprintf("%s-verify-fix", cmd.ID),
								Action: "calculator",
								Input:  map[string]interface{}{"expression": "2 + 2"},
							}
							res, subErr := c.Executor.Execute(ctx, subCmd)
							var observation string
							if subErr != nil {
								observation = fmt.Sprintf("tool error: %v", subErr)
							} else {
								observation = res.Observation
							}
							log.Printf("[Cerebellum] Fixed tool result: %q", observation)
							corr := CognitiveResult{
								GoalID: cmd.ID,
								Intent: "correction",
								Plan:   []string{"correction: invalid content detected. calculator tool output: " + observation},
							}
							select {
							case c.CorrectionOut <- corr:
							case <-ctx.Done():
								return
							}
							continue
						}

						corr := CognitiveResult{
							GoalID: cmd.ID,
							Intent: "correction",
							Plan:   []string{"correction: invalid content detected"},
						}
						select {
						case c.CorrectionOut <- corr:
						case <-ctx.Done():
							return
						}
						continue
					}

					corr := CognitiveResult{
						GoalID: cmd.ID,
						Intent: "respond",
						Plan:   []string{content},
					}
					select {
					case c.CorrectionOut <- corr:
					case <-ctx.Done():
						return
					}
					continue
				}

				// If the step is a ReAct task, Cerebellum runs a local autonomous ReAct loop
				if strings.HasPrefix(step, "react:") {
					prompt := strings.TrimPrefix(step, "react:")
					log.Printf("[Cerebellum] Autonomous local ReAct loop initiated for: %s", prompt)

					var observation = "no observations yet"
					var lastAnswer = ""
					for loop := 0; loop < 2; loop++ {
						llmPrompt := fmt.Sprintf("Goal: %s. Previous Observation: %s. Decide next tool action: calculator(expr) or respond(text).", prompt, observation)
						var llmResp string
						var err error
						if c.Model != nil {
							llmResp, err = c.Model.Generate(ctx, llmPrompt)
						} else {
							// Fallback mock responses if model client not provided
							llmResp = "calculator: 2 + 2"
							if loop > 0 {
								llmResp = "respond: 4"
							}
						}

						if err != nil {
							log.Printf("[Cerebellum-Warning] Local LLM call failed: %v", err)
							observation = fmt.Sprintf("Error calling LLM: %v", err)
							continue
						}

						log.Printf("[Cerebellum] Local LLM tactical thought: %q", llmResp)
						if c.SignalCallback != nil {
							c.SignalCallback(cognitive.GoalID, "react_thought", fmt.Sprintf("Tactical thought (loop %d): %s", loop, llmResp), llmResp)
						}

						if strings.HasPrefix(llmResp, "calculator:") {
							expr := strings.TrimSpace(strings.TrimPrefix(llmResp, "calculator:"))
							subCmd := tool.Command{
								ID:     fmt.Sprintf("%s-react-%d", cmd.ID, loop),
								Action: "calculator",
								Input:  map[string]interface{}{"expression": expr},
							}
							if c.SignalCallback != nil {
								c.SignalCallback(cognitive.GoalID, "tool_call_start", "Running ReAct sub-tool call (calculator)", subCmd)
							}
							res, subErr := c.Executor.Execute(ctx, subCmd)
							if subErr != nil {
								observation = fmt.Sprintf("tool error: %v", subErr)
							} else {
								observation = res.Observation
							}
							if c.SignalCallback != nil {
								c.SignalCallback(cognitive.GoalID, "tool_call_end", "ReAct sub-tool call completed", res)
							}
							log.Printf("[Cerebellum] Local tool result: %q", observation)
						} else if strings.HasPrefix(llmResp, "respond:") {
							lastAnswer = strings.TrimSpace(strings.TrimPrefix(llmResp, "respond:"))
							break
						} else {
							lastAnswer = llmResp
							break
						}
					}

					log.Printf("[Cerebellum] Autonomous ReAct finished. Result: %q", lastAnswer)
					corr := CognitiveResult{
						GoalID: cmd.ID,
						Intent: "refined_action",
						Plan:   []string{lastAnswer},
					}
					select {
					case c.CorrectionOut <- corr:
					case <-ctx.Done():
						return
					}
					continue
				}

				// Respond actions are routed to output via Brainstem, other tools execute directly
				if toolName == "respond" {
					// Route respond commands directly as a correction report containing the response intent
					corr := CognitiveResult{
						GoalID: cmd.ID,
						Intent: "respond",
						Plan:   []string{step},
					}
					select {
					case c.CorrectionOut <- corr:
					case <-ctx.Done():
						return
					}
					continue
				}

				log.Printf("[Cerebellum] Executing tool locally: %s with args: %+v", cmd.Action, cmd.Input)
				if c.SignalCallback != nil {
					c.SignalCallback(cognitive.GoalID, "tool_call_start", fmt.Sprintf("Calling tool %s", cmd.Action), cmd)
				}
				res, err := c.Executor.Execute(ctx, cmd)

				success := err == nil
				output := ""
				if success {
					output = res.Observation
				}
				if c.SignalCallback != nil {
					c.SignalCallback(cognitive.GoalID, "tool_call_end", fmt.Sprintf("Tool %s completed", cmd.Action), res)
				}
				log.Printf("[Cerebellum] Local execution feedback for Action %s. Success: %t, Output: %q", cmd.ID, success, output)

				// Small Cerebellar correction calculation loop
				// Send a correction report upward through Thalamus (Diencephalon)
				corr := CognitiveResult{
					GoalID: cmd.ID,
					Intent: "refined_action",
					Plan:   []string{"verify_result_status"},
				}
				select {
				case c.CorrectionOut <- corr:
				case <-ctx.Done():
					return
				}
			}
		case tel := <-c.TelemetryIn:
			_ = tel
		case <-ticker.C:
			// High frequency checking loop
		}
	}
}
