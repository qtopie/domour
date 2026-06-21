package brain

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Diencephalon Represents the Sensory Relay and LLM Adapter Layer
//
// Role:
//   - Acts as the unified gateway for all Large Language Model (LLM) invocations.
//   - Hides the implementation details of various providers (OpenAI, DeepSeek, Gemini, CLI).
//   - Handles model instantiation, prompt formatting, tool binding, and text generation.
//
// Function:
//   - The Cerebrum and Cerebellum interact with the world through the Diencephalon.
//   - It ensures the cognitive layers remain stateless and decoupled from specific LLM vendors.
//
// Analogy:
//   The thalamus. It relays sensory information (prompts) to the cortex and motor commands back down.

type Diencephalon interface {
	// Generate passes a prompt to the configured LLM and returns the raw response.
	// In reality, this interfaces with `internal/cognitor/proxy.Client`.
	Generate(ctx context.Context, prompt string) (string, error)
}

// DiencephalonNode is the double-loop event node for the Diencephalon component.
// It holds the input/output channels (synapses) and drives independent goroutines
// for sensory intake, routing, and command/correction/response relaying.
type DiencephalonNode struct {
	RawSensoryIn chan SensorySignal
	SensoryRelay chan SensorySignal
	SemanticOut  chan SensorySignal
	TactileOut   chan SensorySignal

	// Thalamus Direct Command Downward Relay
	CommandIn  chan CognitiveResult
	CommandOut chan CognitiveResult

	// Thalamus Upward Correction Relay
	CorrectionIn  chan CognitiveResult
	CorrectionOut chan CognitiveResult

	// Thalamus Final Response Gateway
	ResponseIn  chan MotorFeedback
	ResponseOut chan MotorFeedback

	sessions          map[string]*State
	responseListeners map[string]chan<- MotorFeedback
	mu                sync.Mutex
}

func NewDiencephalonNode() *DiencephalonNode {
	return &DiencephalonNode{
		RawSensoryIn:  make(chan SensorySignal, 100),
		SensoryRelay:  make(chan SensorySignal, 100),
		SemanticOut:   make(chan SensorySignal, 20),
		TactileOut:    make(chan SensorySignal, 50),
		CommandIn:     make(chan CognitiveResult, 10),
		CommandOut:    make(chan CognitiveResult, 10),
		CorrectionIn:  make(chan CognitiveResult, 10),
		CorrectionOut: make(chan CognitiveResult, 10),
		ResponseIn:    make(chan MotorFeedback, 20),
		ResponseOut:   make(chan MotorFeedback, 20),
		sessions:          make(map[string]*State),
		responseListeners: make(map[string]chan<- MotorFeedback),
	}
}

func (d *DiencephalonNode) RegisterListener(sessionID string, ch chan<- MotorFeedback) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.responseListeners == nil {
		d.responseListeners = make(map[string]chan<- MotorFeedback)
	}
	d.responseListeners[sessionID] = ch
}

func (d *DiencephalonNode) UnregisterListener(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.responseListeners != nil {
		delete(d.responseListeners, sessionID)
	}
}

// Start launches both the sensing and routing goroutines for the Diencephalon node.
func (d *DiencephalonNode) Start(ctx context.Context) {
	go d.startSensingLoop(ctx)
	go d.startRoutingLoop(ctx)
	go d.startRelayLoops(ctx)
}

// startSensingLoop runs the Sensory Sensing Loop (感知循环)
func (d *DiencephalonNode) startSensingLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case raw := <-d.RawSensoryIn:
			raw.Timestamp = time.Now()
			reqCtx := raw.Ctx
			if reqCtx == nil {
				reqCtx = ctx
			}
			select {
			case d.SensoryRelay <- raw:
			case <-ctx.Done():
				return
			case <-reqCtx.Done():
			case <-time.After(5 * time.Second):
			}
		}
	}
}

// startRoutingLoop runs the Sensory Routing Loop (路由与分发循环)
func (d *DiencephalonNode) startRoutingLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case signal := <-d.SensoryRelay:
			reqCtx := signal.Ctx
			if reqCtx == nil {
				reqCtx = ctx
			}
			if d.isHighFrequencySignal(signal) {
				select {
				case d.TactileOut <- signal:
				case <-ctx.Done():
					return
				case <-reqCtx.Done():
				case <-time.After(5 * time.Second):
				}
			} else {
				// Convert to global query Event and drive the coordinator
				sessionID := signal.SessionID
				if sessionID == "" {
					sessionID = fmt.Sprintf("G%d", signal.Timestamp.UnixNano())
				}
				state := d.getOrCreateState(sessionID)

				queryStr := fmt.Sprintf("%v", signal.Data)
				if strings.Contains(strings.ToLower(queryStr), "nested") {
					state.ActiveEngine = "plan_execute_nested"
				} else if strings.Contains(strings.ToLower(queryStr), "react") {
					state.ActiveEngine = "react"
				} else if strings.Contains(strings.ToLower(queryStr), "simple") {
					state.ActiveEngine = "simple"
				}

				ev := Event{
					SessionID: sessionID,
					Type:      EventUserQuery,
					Payload:   queryStr,
					Timestamp: signal.Timestamp,
				}
				go d.drive(reqCtx, state, ev)
			}
		}
	}
}

func (d *DiencephalonNode) isHighFrequencySignal(sig SensorySignal) bool {
	return sig.Source == "telemetry" || sig.Source == "sensor"
}

// startRelayLoops handles the bidirectional relay loops of Thalamus gateway
func (d *DiencephalonNode) startRelayLoops(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case cmd := <-d.CommandIn:
			// LLM/Cerebrum response event
			sessionID := cmd.GoalID
			state := d.getOrCreateState(sessionID)
			ev := Event{
				SessionID: sessionID,
				Type:      EventLLMResponse,
				Payload:   cmd.Intent + "\n" + strings.Join(cmd.Plan, "\n"),
				Timestamp: time.Now(),
			}
			go d.drive(ctx, state, ev)

		case corr := <-d.CorrectionIn:
			// Exec result from Cerebellum
			sessionID := getBaseSessionID(corr.GoalID)
			state := d.getOrCreateState(sessionID)
			ev := Event{
				SessionID: sessionID,
				Type:      EventExecResult,
				Payload:   strings.Join(corr.Plan, "\n"),
				Timestamp: time.Now(),
			}
			go d.drive(ctx, state, ev)

		case resp := <-d.ResponseIn:
			d.mu.Lock()
			listener, ok := d.responseListeners[resp.ActionID]
			d.mu.Unlock()
			if ok {
				select {
				case listener <- resp:
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
					log.Printf("[Thalamus-Warning] Listener channel blocked for session %s", resp.ActionID)
				}
			}

			select {
			case d.ResponseOut <- resp:
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func getBaseSessionID(goalID string) string {
	if idx := strings.LastIndex(goalID, "-"); idx != -1 {
		// make sure it ends with a step number suffix (e.g. G12345-0)
		if idx > 1 { // "G" prefix has length 1
			return goalID[:idx]
		}
	}
	return goalID
}

func (d *DiencephalonNode) getOrCreateState(sessionID string) *State {
	d.mu.Lock()
	defer d.mu.Unlock()
	state, ok := d.sessions[sessionID]
	if !ok {
		state = &State{
			SessionID:     sessionID,
			ActiveEngine:  "plan_execute",
			ReasonerState: make(map[string]interface{}),
		}
		d.sessions[sessionID] = state
	}
	return state
}

func (d *DiencephalonNode) drive(ctx context.Context, state *State, ev Event) {
	engine, exists := GetReasoner(state.ActiveEngine)
	if !exists {
		log.Printf("[Thalamus] Engine %s not found, fallback to default", state.ActiveEngine)
		return
	}

	next, err := engine.Decide(ctx, state, ev)
	if err != nil {
		log.Printf("[Thalamus-Error] Decide failed for session %s: %v", state.SessionID, err)
		return
	}

	switch next.Action {
	case ActionCallLLM:
		sig := SensorySignal{
			Ctx:       ctx,
			SessionID: state.SessionID,
			Source:    "thalamus",
			Data:      next.Payload.(string),
			Timestamp: getTimestampFromSessionID(state.SessionID),
		}
		select {
		case d.SemanticOut <- sig:
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			log.Printf("[Thalamus-Warning] SemanticOut blocked.")
		}

	case ActionCallTool:
		cmd := CognitiveResult{
			GoalID: state.SessionID,
			Intent: "execute_tool",
			Plan:   []string{next.Payload.(string)},
		}
		select {
		case d.CommandOut <- cmd:
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			log.Printf("[Thalamus-Warning] CommandOut blocked.")
		}

	case ActionFinish:
		fb := MotorFeedback{
			ActionID: state.SessionID,
			Success:  true,
			Output:   next.Payload.(string),
		}
		select {
		case d.ResponseIn <- fb:
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			log.Printf("[Thalamus-Warning] ResponseIn blocked.")
		}

		d.mu.Lock()
		delete(d.sessions, state.SessionID)
		d.mu.Unlock()
	}
}

func getTimestampFromSessionID(sessionID string) time.Time {
	if strings.HasPrefix(sessionID, "G") {
		var nano int64
		_, err := fmt.Sscanf(sessionID, "G%d", &nano)
		if err == nil {
			return time.Unix(0, nano)
		}
	}
	return time.Now()
}
