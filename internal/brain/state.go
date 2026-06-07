package brain

// TaskStatus defines the lifecycle of a task
type TaskStatus string

const (
	StatusPending   TaskStatus = "pending"
	StatusRunning   TaskStatus = "running"
	StatusCompleted TaskStatus = "completed"
	StatusFailed    TaskStatus = "failed"
)

// TaskStep represents an atomic task definition
type TaskStep struct {
	ID          string                 `json:"id"`
	Action      string                 `json:"action"` // Corresponding key for the Worker
	Input       map[string]interface{} `json:"input"`
	Status      TaskStatus             `json:"status"`
	Observation string                 `json:"observation"` // Execution feedback
}

// State represents the global context dashboard of the brain
type State struct {
	SessionID     string
	GlobalGoal    string
	Complexity    int // Score from 1-10
	Steps         []*TaskStep
	CurrentStepID string
	UserFeedback  string     // Stores new instructions from the user during execution
	Mode          SystemMode // Current system operating mode

	ReasonerState map[string]interface{}
	History       []Event
	ActiveEngine  string
}

const (
	ComplexitySimple  = 1 // Direct execution
	ComplexityGeneral = 5 // ReAct loop
	ComplexityComplex = 9 // Planner/Worker orchestration
)

// SystemMode defines the system operating mode, balancing cognitive power (LLM) and bionic energy (I/O)
type SystemMode string

const (
	// ModeHibernate Hibernate Mode: Zero energy consumption, cognitive layer shut down, only scheduled wakeup remains.
	ModeHibernate SystemMode = "hibernate"
	// ModeCasual Casual/Daily Mode: Heartbeat and basic response, low frequency LLM and I/O usage.
	ModeCasual SystemMode = "casual"
	// ModeBalanced Balanced Mode: Optimal balance between experience and energy consumption.
	ModeBalanced SystemMode = "balanced"
	// ModePerformance Performance Mode: Maximum throughput and minimum latency, parallel execution, io_uring enabled.
	ModePerformance SystemMode = "performance"

	// Advanced Bionic / Edge Scenarios

	// ModeVigilant Vigilant Mode: Edge perception and reflex arcs. Cognitive layer suspended, bionic layer highly sensitive.
	ModeVigilant SystemMode = "vigilant"
	// ModeSurvival Survival Mode: Offline autonomy. Local-only small models, ensuring basic execution without cloud connectivity.
	ModeSurvival SystemMode = "survival"
	// ModeDeepThink Deep Think Mode: Offline self-evolution. Body still, full compute dedicated to long-chain reasoning/reflection.
	ModeDeepThink SystemMode = "deep_think"
	// ModeStealth Stealth Mode: Absolute privacy and compliance. Local encryption, strict I/O de-sensitization.
	ModeStealth SystemMode = "stealth"
	// ModeDiagnostic Diagnostic Mode: Traceability and debugging. Normal cognition, but I/O goes to Sandbox/Mock.
	ModeDiagnostic SystemMode = "diagnostic"
)

// PowerLevel represents the intensity of energy or compute allocated to a component
type PowerLevel string

const (
	PowerOff       PowerLevel = "off"
	PowerLow       PowerLevel = "low"
	PowerNormal    PowerLevel = "normal"
	PowerHigh      PowerLevel = "high"
	PowerLocal     PowerLevel = "local_only"
	PowerSandbox   PowerLevel = "sandbox"
	PowerEncrypted PowerLevel = "encrypted"
)

// ModeProfile describes the resource allocation matrix for a specific system mode
type ModeProfile struct {
	CognitivePower PowerLevel // Brain compute (LLM/Cognitive)
	BionicPower    PowerLevel // Body/Kernel I/O (Bionic)
	Description    string
}

// GetModeProfile returns the resource allocation matrix for the corresponding mode
func GetModeProfile(mode SystemMode) ModeProfile {
	switch mode {
	case ModeHibernate:
		return ModeProfile{PowerOff, PowerOff, "Total sleep, zero energy consumption."}
	case ModeVigilant:
		return ModeProfile{PowerLow, PowerHigh, "Brain suspended, eBPF/Sensors highly active, ready for reflex."}
	case ModeCasual:
		return ModeProfile{PowerLow, PowerLow, "Maintaining heartbeat, low-frequency responses."}
	case ModeSurvival:
		return ModeProfile{PowerLocal, PowerNormal, "Offline autonomy, fully regressed to edge-side models and local storage."}
	case ModeBalanced:
		return ModeProfile{PowerNormal, PowerNormal, "Standard interaction, optimal balance of experience and energy."}
	case ModePerformance:
		return ModeProfile{PowerHigh, PowerHigh, "Maximum concurrency, all models open, unrestricted I/O."}
	case ModeDeepThink:
		return ModeProfile{PowerHigh, PowerOff, "Physical stillness, full power dedicated to reasoning and knowledge reconstruction."}
	case ModeStealth:
		return ModeProfile{PowerNormal, PowerEncrypted, "Cut off telemetry, strict information filtering, memory-level encrypted flow."}
	case ModeDiagnostic:
		return ModeProfile{PowerNormal, PowerSandbox, "Sandbox environment, physical output intercepted or redirected."}
	default:
		return ModeProfile{PowerNormal, PowerNormal, "Default balanced mode."}
	}
}
