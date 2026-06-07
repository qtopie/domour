package brain

import (
	"context"
	"log"
	"strings"
	"time"
)

// Cerebrum Represents the Cognitive and Inference Center (System 2)
//
// Role:
//   - High-level semantic understanding and intent completion.
//   - Task breakdown, planning, and self-reflection.
//   - Proposes intermediate semantic events (e.g., plans, suggestions).
//
// Constraints:
//   - Does NOT have the right to execute final physical actions.
//   - Does NOT directly render UI outputs.
//   - It thinks; it does not do.
//
// Analogy:
//   The "System 2" slow thinker. It maps out the DAG of tasks for complex objectives.

type Cerebrum interface {
	// Plan receives a complex goal and breaks it down into an actionable plan.
	Plan(ctx context.Context, goal string) (string, error)

	// Reflect evaluates the outcome of an execution sequence and suggests corrections.
	Reflect(ctx context.Context, observation string) (string, error)
}

// CerebrumNode is the double-goroutine event node for the Cerebrum component.
// It holds two independent channel sets: one for slow cognitive reasoning (System 2),
// and one for asynchronous memory operations, keeping them from blocking each other.
type CerebrumNode struct {
	TaskIn       chan CognitiveTask
	CorrectionIn chan CognitiveResult
	ResultOut    chan CognitiveResult
	MemoryCmdOut chan MemoryCommand
}

func NewCerebrumNode() *CerebrumNode {
	return &CerebrumNode{
		TaskIn:       make(chan CognitiveTask, 10),
		CorrectionIn: make(chan CognitiveResult, 10),
		ResultOut:    make(chan CognitiveResult, 10),
		MemoryCmdOut: make(chan MemoryCommand, 50),
	}
}

// Start launches both the thinking and memory goroutines for the Cerebrum node.
func (c *CerebrumNode) Start(ctx context.Context) {
	go c.startThinkingLoop(ctx)
	go c.startMemoryLoop(ctx)
}

// startThinkingLoop runs the Thinking Slow Loop (思考慢循环 / System 2)
// startThinkingLoop runs the Thinking Slow Loop (思考慢循环 / System 2).
// In the decoupled architecture, it acts as a stateless planner/reflector
// producing cognitive plans or assessments without managing execution state.
func (c *CerebrumNode) startThinkingLoop(ctx context.Context) {
	log.Println("[Cerebrum] Thinking loop started.")
	for {
		select {
		case <-ctx.Done():
			log.Println("[Cerebrum] Thinking loop stopped.")
			return
		case task := <-c.TaskIn:
			taskCtx := task.Ctx
			if taskCtx == nil {
				taskCtx = ctx
			}

			// Check if cancelled before starting
			select {
			case <-taskCtx.Done():
				log.Printf("[Cerebrum] Task %s already cancelled, skipping.", task.GoalID)
				continue
			default:
			}

			log.Printf("[Cerebrum] Analyzing cognitive goal: %s (GoalID: %s)", task.Prompt, task.GoalID)

			// Simulating thinking with cancellation support
			select {
			case <-taskCtx.Done():
				log.Printf("[Cerebrum] Thinking interrupted for task %s.", task.GoalID)
				continue
			case <-time.After(150 * time.Millisecond):
			}

			plan := []string{"inspect_env", "trigger_math_calculators"}
			if strings.Contains(task.Prompt, "nested") {
				plan = []string{"react:inspect_env", "trigger_math_calculators"}
			}

			intent := "execute_workflow"
			if strings.Contains(task.Prompt, "Observation:") || strings.Contains(task.Prompt, "verify_result_status") {
				intent = "respond"
				plan = []string{"respond"}
			} else if strings.Contains(task.Prompt, "correction:") {
				plan = []string{"corrected result: 2+2=4"}
			} else if strings.Contains(task.Prompt, "simple") {
				if strings.Contains(task.Prompt, "invalid") || strings.Contains(task.Prompt, "problem") {
					plan = []string{"invalid result: 2+2=5"}
				} else {
					plan = []string{"result: 4"}
				}
			}

			result := CognitiveResult{
				GoalID: task.GoalID,
				Intent: intent,
				Plan:   plan,
			}
			select {
			case c.ResultOut <- result:
			case <-ctx.Done():
				return
			case <-taskCtx.Done():
				log.Printf("[Cerebrum] Dropped result for task %s: context cancelled", task.GoalID)
			case <-time.After(5 * time.Second):
				log.Printf("[Cerebrum-Warning] ResultOut channel blocked. Dropping result.")
			}
		case corr := <-c.CorrectionIn:
			log.Printf("[Cerebrum] Processing correction feedback for Goal: %s, Plan: %v", corr.GoalID, corr.Plan)
			// Simulate path refinement based on correction report
			time.Sleep(100 * time.Millisecond)
			result := CognitiveResult{
				GoalID: corr.GoalID,
				Intent: "respond",
				Plan:   []string{"respond"},
			}
			select {
			case c.ResultOut <- result:
			case <-ctx.Done():
				return
			}
		}
	}
}

// startMemoryLoop runs the Memory Async Loop (记忆异步循环)
func (c *CerebrumNode) startMemoryLoop(ctx context.Context) {
	log.Println("[Cerebrum] Memory loop started.")
	for {
		select {
		case <-ctx.Done():
			log.Println("[Cerebrum] Memory loop stopped.")
			return
		case cmd := <-c.MemoryCmdOut:
			switch cmd.Op {
			case "store":
				log.Printf("[Cerebrum-Memory] Storing payload: %s", cmd.Payload.Content)
			case "recall":
				log.Printf("[Cerebrum-Memory] Recalling memories...")
				if cmd.ReplyCh != nil {
					cmd.ReplyCh <- []MemoryPayload{}
				}
			}
		}
	}
}
