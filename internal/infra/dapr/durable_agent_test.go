package dapr

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/dapr/go-sdk/workflow"
)

type StepInput struct {
	InstanceID string
	Data       string
}

var (
	mu             sync.Mutex
	executedSteps  = make(map[string][]string) // maps instanceID -> list of steps executed
	step2RunCounts = make(map[string]int32)     // maps instanceID -> number of times Step 2 executed
	step2Started   = make(chan struct{}, 1)
)

func recordStep(instanceID string, name string) {
	mu.Lock()
	defer mu.Unlock()
	executedSteps[instanceID] = append(executedSteps[instanceID], name)
}

func getExecutedSteps(instanceID string) []string {
	mu.Lock()
	defer mu.Unlock()
	steps := executedSteps[instanceID]
	res := make([]string, len(steps))
	copy(res, steps)
	return res
}

func Step1Activity(ctx workflow.ActivityContext) (any, error) {
	var input StepInput
	if err := ctx.GetInput(&input); err != nil {
		return nil, err
	}
	recordStep(input.InstanceID, "Step1")
	return fmt.Sprintf("Step1Result(%s)", input.Data), nil
}

func Step2Activity(ctx workflow.ActivityContext) (any, error) {
	var input StepInput
	if err := ctx.GetInput(&input); err != nil {
		return nil, err
	}
	recordStep(input.InstanceID, "Step2")

	mu.Lock()
	count := step2RunCounts[input.InstanceID]
	count++
	step2RunCounts[input.InstanceID] = count
	mu.Unlock()

	if count == 1 {
		// First execution for this instance ID: notify the test main thread to interrupt us
		// and then block to simulate process interruption.
		select {
		case step2Started <- struct{}{}:
		default:
		}
		// Block until context is canceled or we are terminated
		<-ctx.Context().Done()
		return nil, ctx.Context().Err()
	}
	return fmt.Sprintf("Step2Result(%s)", input.Data), nil
}

func Step3Activity(ctx workflow.ActivityContext) (any, error) {
	var input StepInput
	if err := ctx.GetInput(&input); err != nil {
		return nil, err
	}
	recordStep(input.InstanceID, "Step3")
	return fmt.Sprintf("Step3Result(%s)", input.Data), nil
}

func DurableAgentTaskWorkflow(ctx *workflow.WorkflowContext) (any, error) {
	var input string
	if err := ctx.GetInput(&input); err != nil {
		return nil, err
	}

	instID := ctx.InstanceID()

	var res1 string
	input1 := StepInput{InstanceID: instID, Data: input}
	if err := ctx.CallActivity(Step1Activity, workflow.ActivityInput(input1)).Await(&res1); err != nil {
		return nil, err
	}

	var res2 string
	input2 := StepInput{InstanceID: instID, Data: res1}
	if err := ctx.CallActivity(Step2Activity, workflow.ActivityInput(input2)).Await(&res2); err != nil {
		return nil, err
	}

	var res3 string
	input3 := StepInput{InstanceID: instID, Data: res2}
	if err := ctx.CallActivity(Step3Activity, workflow.ActivityInput(input3)).Await(&res3); err != nil {
		return nil, err
	}

	return res3, nil
}

func TestDurableAgentRecovery(t *testing.T) {
	// 1. Create and start DurableAgent Worker 1
	agent1, err := NewDurableAgent("Stevie-1")
	if err != nil {
		t.Fatalf("failed to create agent1: %v", err)
	}

	if err := agent1.RegisterWorkflow(DurableAgentTaskWorkflow); err != nil {
		t.Fatalf("failed to register workflow: %v", err)
	}
	if err := agent1.RegisterStep(Step1Activity); err != nil {
		t.Fatalf("failed to register step 1: %v", err)
	}
	if err := agent1.RegisterStep(Step2Activity); err != nil {
		t.Fatalf("failed to register step 2: %v", err)
	}
	if err := agent1.RegisterStep(Step3Activity); err != nil {
		t.Fatalf("failed to register step 3: %v", err)
	}

	if err := agent1.Start(); err != nil {
		t.Fatalf("failed to start agent1: %v", err)
	}

	// 2. Create Dapr Client to start the workflow
	cli, err := NewDurableAgentClient()
	if err != nil {
		agent1.Shutdown()
		t.Fatalf("failed to create dapr client: %v", err)
	}
	defer cli.Close()

	ctx := context.Background()
	instanceID := fmt.Sprintf("durable-task-recovery-%d", time.Now().UnixNano())

	// Start the workflow task
	_, err = cli.StartAgentTask(ctx, "DurableAgentTaskWorkflow", instanceID, "initial-input")
	if err != nil {
		agent1.Shutdown()
		t.Fatalf("failed to start agent task: %v", err)
	}

	// 3. Wait until Step 2 has started
	select {
	case <-step2Started:
		// Step 2 has started executing on Worker 1.
		// Now we interrupt the task by shutting down Worker 1!
		t.Log("Step 2 started. Shutting down Worker 1 to simulate task/process interruption...")
		agent1.Shutdown()
	case <-time.After(15 * time.Second):
		agent1.Shutdown()
		t.Fatalf("timeout waiting for Step 2 to start")
	}

	// Ensure Worker 1 has stopped. Let's wait a moment.
	time.Sleep(2 * time.Second)

	// Verify the steps executed so far: Step 1 and Step 2 (first attempt)
	stepsBefore := getExecutedSteps(instanceID)
	t.Logf("Executed steps before restart: %v", stepsBefore)

	// 4. Create and start DurableAgent Worker 2 (restarts/recovers the task)
	t.Log("Starting Worker 2 to resume and recover the interrupted task...")
	agent2, err := NewDurableAgent("Stevie-2")
	if err != nil {
		t.Fatalf("failed to create agent2: %v", err)
	}
	defer agent2.Shutdown()

	if err := agent2.RegisterWorkflow(DurableAgentTaskWorkflow); err != nil {
		t.Fatalf("failed to register workflow: %v", err)
	}
	if err := agent2.RegisterStep(Step1Activity); err != nil {
		t.Fatalf("failed to register step 1: %v", err)
	}
	if err := agent2.RegisterStep(Step2Activity); err != nil {
		t.Fatalf("failed to register step 2: %v", err)
	}
	if err := agent2.RegisterStep(Step3Activity); err != nil {
		t.Fatalf("failed to register step 3: %v", err)
	}

	if err := agent2.Start(); err != nil {
		t.Fatalf("failed to start agent2: %v", err)
	}

	// 5. Poll the workflow status until it is completed
	t.Log("Polling workflow task status until completion...")
	var metadata *workflow.Metadata
	for i := 0; i < 20; i++ {
		metadata, err = cli.GetTaskStatus(ctx, instanceID)
		if err != nil {
			t.Fatalf("failed to get task status: %v", err)
		}
		t.Logf("Workflow status: %s", metadata.RuntimeStatus)
		if metadata.RuntimeStatus == workflow.StatusCompleted || metadata.RuntimeStatus == workflow.StatusFailed || metadata.RuntimeStatus == workflow.StatusTerminated {
			break
		}
		time.Sleep(2 * time.Second)
	}

	if metadata.RuntimeStatus != workflow.StatusCompleted {
		t.Fatalf("Workflow did not complete, final status: %s", metadata.RuntimeStatus)
	}

	// 6. Verify executed steps.
	// Expected steps:
	// - Step1 (run by Worker 1)
	// - Step2 (interrupted in Worker 1)
	// On Worker 2 run:
	// - Step1 is NOT re-executed (Dapr plays it back from history without calling Step1Activity).
	// - Step2 is re-executed (since the first execution did not complete successfully).
	// - Step3 is executed.
	// So `executedSteps` should contain: ["Step1", "Step2", "Step2", "Step3"].
	// BUT "Step1" should appear EXACTLY ONCE!
	stepsAfter := getExecutedSteps(instanceID)
	t.Logf("Final executed steps: %v", stepsAfter)

	step1Count := 0
	step2Count := 0
	step3Count := 0
	for _, step := range stepsAfter {
		switch step {
		case "Step1":
			step1Count++
		case "Step2":
			step2Count++
		case "Step3":
			step3Count++
		}
	}

	if step1Count != 1 {
		t.Errorf("Expected Step1 to be executed exactly once (demonstrating it was not replayed), but got %d", step1Count)
	}
	if step2Count < 2 {
		t.Errorf("Expected Step2 to be executed at least twice (first failed/interrupted, second recovered), but got %d", step2Count)
	}
	if step3Count != 1 {
		t.Errorf("Expected Step3 to be executed exactly once, but got %d", step3Count)
	}

	t.Log("DurableAgent recovery test passed successfully!")
}
