package brain

import "github.com/qtopie/domour/ark/governor"

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
	SessionID    string
	TopicID      string // Topic-scoped conversation ID, set by TopicDetector (e.g. "sess_abc:1")
	CurrentTopic string // Human-readable topic label (top-3 weighted terms)
	GlobalGoal   string
	Complexity   int // Score from 1-10
	Steps        []*TaskStep
	CurrentStepID  string
	UserFeedback   string     // Stores new instructions from the user during execution
	Mode           SystemMode // Current system operating mode

	ReasonerState map[string]interface{}
	History       []Event
	ActiveEngine  string

	// Topic tracking — Diencephalon uses this to detect shifts across turns.
	previousTopicFP *TopicFingerprint
	topicSeq        int

	// ReAct loop guard
	ToolCallCount int // Incremented each time a tool is dispatched
	MaxToolCalls  int // 0 = unlimited (default). When exceeded, Cerebrum reviews.
}

const (
	ComplexitySimple  = 1 // Direct execution
	ComplexityGeneral = 5 // ReAct loop
	ComplexityComplex = 9 // Planner/Worker orchestration
)

// SystemMode defines the system operating mode, balancing cognitive power (LLM) and bionic energy (I/O)
type SystemMode = governor.SystemMode

const (
	ModeHibernate   SystemMode = governor.ModeHibernate
	ModeCasual      SystemMode = governor.ModeCasual
	ModeBalanced    SystemMode = governor.ModeBalanced
	ModePerformance SystemMode = governor.ModePerformance
	ModeVigilant    SystemMode = governor.ModeVigilant
	ModeSurvival    SystemMode = governor.ModeSurvival
	ModeDeepThink   SystemMode = governor.ModeDeepThink
	ModeStealth     SystemMode = governor.ModeStealth
	ModeDiagnostic  SystemMode = governor.ModeDiagnostic
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
