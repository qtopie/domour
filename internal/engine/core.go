package engine

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/brain"
	_ "github.com/qtopie/domour/internal/reasoning/planner"
	_ "github.com/qtopie/domour/internal/reasoning/react"
	_ "github.com/qtopie/domour/internal/reasoning/simple"
)

// Engine is the top-level runtime coordinator. It aggregates the LLM gateway (Brain)
// and the physical execution layer (Motor), and owns the lifecycle of all four
// biomorphic neural component nodes.
//
// I/O contract:
//   - Submit  → sends a sensory signal into the Diencephalon (sole entry point)
//   - Results → receives final responses from the Diencephalon (sole output gateway)
type Engine interface {
	Cognitor() CognitorClient
	Executor() ExecutorClient

	// Start launches all concurrent neural event loops. Non-blocking.
	Start(ctx context.Context) error

	// Submit injects an external signal into the system via the Diencephalon.
	Submit(ctx context.Context, signal brain.SensorySignal) error

	// Results returns the read-only channel of final responses produced by the Diencephalon.
	Results() <-chan brain.MotorFeedback

	AddObserver(sessionID string, obs SignalObserver)
	RemoveObserver(sessionID string)
	Diencephalon() *brain.DiencephalonNode
}

type SignalEvent struct {
	SessionID   string
	Timestamp   time.Time
	FromNode    string
	ToNode      string
	EventType   string
	Description string
	Payload     any
}

type SignalObserver func(event SignalEvent)

type engineModelClient struct {
	client CognitorClient
}

func (e *engineModelClient) Generate(ctx context.Context, prompt string) (string, error) {
	if e.client == nil {
		return "", fmt.Errorf("cognitor client is nil")
	}
	cl, err := e.client.GetClient(ctx, "chat")
	if err != nil {
		return "", err
	}
	resp, err := cl.GenerateText(ctx, []*schema.Message{
		schema.UserMessage(prompt),
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// coreEngine is the concrete Engine implementation of the biomorphic multi-loop architecture.
type coreEngine struct {
	brain        CognitorClient
	motor        ExecutorClient
	diencephalon *brain.DiencephalonNode
	cerebrum     *brain.CerebrumNode
	cerebellum   *brain.CerebellumNode
	brainstem    *brain.BrainstemNode

	observers map[string]SignalObserver
	obsMu     sync.RWMutex
}

// NewEngine constructs a new Engine instance with the given LLM and motor clients.
func NewEngine(brainClient CognitorClient, motor ExecutorClient) Engine {
	var modelClient brain.ModelClient
	if brainClient != nil {
		modelClient = &engineModelClient{client: brainClient}
	}
	e := &coreEngine{
		brain:        brainClient,
		motor:        motor,
		diencephalon: brain.NewDiencephalonNode(),
		cerebrum:     brain.NewCerebrumNode(),
		cerebellum:   brain.NewCerebellumNode(motor, modelClient),
		brainstem:    brain.NewBrainstemNode(),
		observers:    make(map[string]SignalObserver),
	}
	e.cerebellum.SignalCallback = func(sessionID string, eventType string, desc string, payload any) {
		e.notifyObservers(sessionID, SignalEvent{
			SessionID:   sessionID,
			Timestamp:   time.Now(),
			FromNode:    "cerebellum",
			ToNode:      "motor",
			EventType:   eventType,
			Description: desc,
			Payload:     payload,
		})
	}
	return e
}

func (e *coreEngine) AddObserver(sessionID string, obs SignalObserver) {
	e.obsMu.Lock()
	defer e.obsMu.Unlock()
	e.observers[sessionID] = obs
}

func (e *coreEngine) RemoveObserver(sessionID string) {
	e.obsMu.Lock()
	defer e.obsMu.Unlock()
	delete(e.observers, sessionID)
}

func (e *coreEngine) notifyObservers(sessionID string, ev SignalEvent) {
	e.obsMu.RLock()
	obs, ok := e.observers[sessionID]
	e.obsMu.RUnlock()
	if ok {
		obs(ev)
	}
}

func (e *coreEngine) Cognitor() CognitorClient {
	return e.brain
}

func (e *coreEngine) Diencephalon() *brain.DiencephalonNode {
	return e.diencephalon
}

func (e *coreEngine) Executor() ExecutorClient {
	return e.motor
}

// Start launches all neural event loop goroutines. Non-blocking.
func (e *coreEngine) Start(ctx context.Context) error {
	log.Println("[Engine] Starting biomorphic multi-node runtime...")

	e.brainstem.RouteFn = e.routeNeuroSignals

	e.diencephalon.Start(ctx)
	e.cerebrum.Start(ctx)
	e.cerebellum.Start(ctx)
	e.brainstem.Start(ctx) // starts routing, Pons, and execution loops

	log.Println("[Engine] All neural event loops spawned.")
	return nil
}

// Submit injects a sensory signal into the system via the Diencephalon.
func (e *coreEngine) Submit(ctx context.Context, signal brain.SensorySignal) error {
	signal.Ctx = ctx
	select {
	case e.diencephalon.RawSensoryIn <- signal:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Results returns the read-only channel of final responses from the Diencephalon.
func (e *coreEngine) Results() <-chan brain.MotorFeedback {
	return e.diencephalon.ResponseOut
}

// routeNeuroSignals bridges inter-node channels, simulating neural signal transmission.
//
// Signal pathways (神经信号通路):
//
//	diencephalon.SemanticOut   ──► cerebrum.TaskIn          (sensory input → cognitive reasoning)
//	diencephalon.TactileOut    ──► cerebellum.TelemetryIn   (tactile telemetry → motor reflexes)
//	cerebrum.ResultOut         ──► diencephalon.CommandIn   (cognitive plan → Thalamus relay)
//	diencephalon.CommandOut    ──► brainstem.CommandIn      (Thalamus relay → Brainstem)
//	brainstem.PonsOut          ──► cerebellum.CognitiveIn   (Pons split copy → Cerebellum side-path)
//	cerebellum.CorrectionOut   ──► diencephalon.CorrectionIn(Cerebellar correction → Thalamus upward relay)
//	diencephalon.CorrectionOut ──► cerebrum.CorrectionIn     (Thalamus relayed correction → Cerebrum)
//	brainstem.ResponseOut      ──► diencephalon.ResponseIn    (final execution response → Thalamus output gateway)
func (e *coreEngine) routeNeuroSignals(ctx context.Context) {
	// 1. diencephalon.SemanticOut ──► cerebrum.TaskIn
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case sig, ok := <-e.diencephalon.SemanticOut:
				if !ok {
					return
				}
				taskCtx := sig.Ctx
				if taskCtx == nil {
					taskCtx = ctx
				}
				sessionID := sig.SessionID
				if sessionID == "" {
					sessionID = fmt.Sprintf("G%d", sig.Timestamp.UnixNano())
				}
				task := brain.CognitiveTask{
					Ctx:     taskCtx,
					GoalID:  sessionID,
					Prompt:  fmt.Sprintf("%v", sig.Data),
					Context: sig.Data,
				}
				e.notifyObservers(sessionID, SignalEvent{
					SessionID:   sessionID,
					Timestamp:   time.Now(),
					FromNode:    "diencephalon",
					ToNode:      "cerebrum",
					EventType:   "sensory_relay",
					Description: "Relaying user query to Cerebrum for macro planning",
					Payload:     sig.Data,
				})
				select {
				case e.cerebrum.TaskIn <- task:
				case <-taskCtx.Done():
					log.Printf("[Engine] Dropped SemanticOut task routing: context cancelled/timed out")
				case <-time.After(5 * time.Second):
					log.Printf("[Engine-Warning] Cerebrum.TaskIn channel blocked on semantic task write. Dropping.")
				}
			}
		}
	}()

	// 2. diencephalon.TactileOut ──► cerebellum.TelemetryIn
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case sig, ok := <-e.diencephalon.TactileOut:
				if !ok {
					return
				}
				sessionID := sig.SessionID
				if sessionID == "" {
					sessionID = fmt.Sprintf("G%d", sig.Timestamp.UnixNano())
				}
				e.notifyObservers(sessionID, SignalEvent{
					SessionID:   sessionID,
					Timestamp:   time.Now(),
					FromNode:    "diencephalon",
					ToNode:      "cerebellum",
					EventType:   "telemetry_relay",
					Description: "Relaying tactile telemetry signal to Cerebellum",
					Payload:     sig.Data,
				})
				brain.EvictAndPushChannel(e.cerebellum.TelemetryIn, sig)
			}
		}
	}()

	// 3. cerebrum.ResultOut ──► diencephalon.CommandIn
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case cogResult, ok := <-e.cerebrum.ResultOut:
				if !ok {
					return
				}
				e.notifyObservers(cogResult.GoalID, SignalEvent{
					SessionID:   cogResult.GoalID,
					Timestamp:   time.Now(),
					FromNode:    "cerebrum",
					ToNode:      "diencephalon",
					EventType:   "cognitive_plan",
					Description: fmt.Sprintf("Cerebrum produced plan: %v", cogResult.Plan),
					Payload:     cogResult,
				})
				select {
				case e.diencephalon.CommandIn <- cogResult:
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
					log.Printf("[Engine-Warning] Diencephalon.CommandIn blocked. Dropping CognitiveResult.")
				}
			}
		}
	}()

	// 4. diencephalon.CommandOut ──► brainstem.CommandIn
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case cmdRelay, ok := <-e.diencephalon.CommandOut:
				if !ok {
					return
				}
				e.notifyObservers(cmdRelay.GoalID, SignalEvent{
					SessionID:   cmdRelay.GoalID,
					Timestamp:   time.Now(),
					FromNode:    "diencephalon",
					ToNode:      "brainstem",
					EventType:   "command_relay",
					Description: fmt.Sprintf("Diencephalon relaying command to Brainstem (Intent: %s)", cmdRelay.Intent),
					Payload:     cmdRelay,
				})
				select {
				case e.brainstem.CommandIn <- cmdRelay:
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
					log.Printf("[Engine-Warning] Brainstem.CommandIn blocked. Dropping Command.")
				}
			}
		}
	}()

	// 5. brainstem.PonsOut ──► cerebellum.CognitiveIn
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ponsCopy, ok := <-e.brainstem.PonsOut:
				if !ok {
					return
				}
				e.notifyObservers(ponsCopy.GoalID, SignalEvent{
					SessionID:   ponsCopy.GoalID,
					Timestamp:   time.Now(),
					FromNode:    "brainstem",
					ToNode:      "cerebellum",
					EventType:   "pons_split",
					Description: "Pons split copy sent to Cerebellum",
					Payload:     ponsCopy,
				})
				select {
				case e.cerebellum.CognitiveIn <- ponsCopy:
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
					log.Printf("[Engine-Warning] Cerebellum.CognitiveIn blocked. Dropping Pons Copy.")
				}
			}
		}
	}()

	// 6. cerebellum.CorrectionOut ──► diencephalon.CorrectionIn
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case correction, ok := <-e.cerebellum.CorrectionOut:
				if !ok {
					return
				}
				e.notifyObservers(correction.GoalID, SignalEvent{
					SessionID:   correction.GoalID,
					Timestamp:   time.Now(),
					FromNode:    "cerebellum",
					ToNode:      "diencephalon",
					EventType:   "correction_report",
					Description: fmt.Sprintf("Cerebellum reported feedback/correction: %s", correction.Intent),
					Payload:     correction,
				})
				select {
				case e.diencephalon.CorrectionIn <- correction:
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
					log.Printf("[Engine-Warning] Diencephalon.CorrectionIn blocked. Dropping Correction.")
				}
			}
		}
	}()

	// 7. diencephalon.CorrectionOut ──► cerebrum.CorrectionIn
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case corrRelay, ok := <-e.diencephalon.CorrectionOut:
				if !ok {
					return
				}
				e.notifyObservers(corrRelay.GoalID, SignalEvent{
					SessionID:   corrRelay.GoalID,
					Timestamp:   time.Now(),
					FromNode:    "diencephalon",
					ToNode:      "cerebrum",
					EventType:   "correction_relay",
					Description: "Diencephalon relaying correction upward to Cerebrum",
					Payload:     corrRelay,
				})
				select {
				case e.cerebrum.CorrectionIn <- corrRelay:
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
					log.Printf("[Engine-Warning] Cerebrum.CorrectionIn blocked. Dropping Correction Relay.")
				}
			}
		}
	}()

	// 8. brainstem.ResponseOut ──► diencephalon.ResponseIn
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case finalResp, ok := <-e.brainstem.ResponseOut:
				if !ok {
					return
				}
				e.notifyObservers(finalResp.ActionID, SignalEvent{
					SessionID:   finalResp.ActionID,
					Timestamp:   time.Now(),
					FromNode:    "brainstem",
					ToNode:      "diencephalon",
					EventType:   "response_relay",
					Description: "Relaying final response from Brainstem to Diencephalon gateway",
					Payload:     finalResp.Output,
				})
				select {
				case e.diencephalon.ResponseIn <- finalResp:
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
					log.Printf("[Engine-Warning] Diencephalon.ResponseIn blocked. Dropping MotorFeedback.")
				}
			}
		}
	}()
}