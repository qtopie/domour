package context

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qtopie/domour/internal/bionic/memory"
	"github.com/qtopie/domour/internal/bionic/tool"
)

// ---------------------------------------------------------------------------
// STM (Short-Term Memory) Ring Buffer
// ---------------------------------------------------------------------------

// STMConfig defines the ring buffer parameters.
type STMConfig struct {
	Capacity        int           // Max nodes in buffer
	ProtectedTurns  int           // Number of recent turns that skip compression
	TokenBudget     int           // Max total tokens
	SummaryInterval int           // Compress every N non-protected turns
}

// DefaultSTMConfig returns sensible defaults for the STM window.
func DefaultSTMConfig() STMConfig {
	return STMConfig{
		Capacity:        2048,
		ProtectedTurns:  2,
		TokenBudget:     128000,
		SummaryInterval: 5,
	}
}

// STMBuffer is a ring buffer that manages short-term memory.
// It maintains a "protected zone" (most recent N turns, no compression)
// and a "compressible zone" (older turns, eligible for summarization/truncation).
type STMBuffer struct {
	config STMConfig
	nodes  []*ConcreteNode
	mu     sync.RWMutex
	offset int // ring buffer write offset
	size   int // total nodes stored
}

// NewSTMBuffer creates a ring buffer with the given capacity.
func NewSTMBuffer(cfg STMConfig) *STMBuffer {
	return &STMBuffer{
		config: cfg,
		nodes:  make([]*ConcreteNode, cfg.Capacity),
	}
}

// Append adds a node to the ring buffer.
func (b *STMBuffer) Append(node *ConcreteNode) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nodes[b.offset] = node
	b.offset = (b.offset + 1) % b.config.Capacity
	if b.size < b.config.Capacity {
		b.size++
	}
}

// Range iterates over all nodes in insertion order.
func (b *STMBuffer) Range(fn func(int, *ConcreteNode) bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.size < b.config.Capacity {
		for i := 0; i < b.size; i++ {
			if b.nodes[i] == nil {
				continue
			}
			if !fn(i, b.nodes[i]) {
				return
			}
		}
		return
	}

	// Ring wrapped: start from offset, wrap around
	for i := 0; i < b.config.Capacity; i++ {
		idx := (b.offset + i) % b.config.Capacity
		if b.nodes[idx] == nil {
			continue
		}
		if !fn(i, b.nodes[idx]) {
			return
		}
	}
}

// Snapshot returns a stable slice of all nodes in insertion order.
func (b *STMBuffer) Snapshot() []*ConcreteNode {
	var result []*ConcreteNode
	b.Range(func(_ int, node *ConcreteNode) bool {
		result = append(result, node)
		return true
	})
	return result
}

// ProtectedBoundary returns the index of the first protected node.
// Protected nodes are the most recent N turns.
func (b *STMBuffer) ProtectedBoundary() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.size == 0 {
		return 0
	}
	// Count unique turns from the end
	seen := make(map[string]bool)
	protected := 0
	nodes := b.Snapshot()
	for i := len(nodes) - 1; i >= 0; i-- {
		if nodes[i] == nil {
			continue
		}
		if !seen[nodes[i].TurnID] {
			seen[nodes[i].TurnID] = true
			protected++
		}
		if protected > b.config.ProtectedTurns {
			return i + 1
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// ContextManager — Per-Agent Session Context
// ---------------------------------------------------------------------------

// ContextManager is the per-Agent session context management.
// Each Agent instance gets its own ContextManager.
//
// Responsibilities:
//   - Maintain Pristine Graph (immutable backup: "what actually happened")
//   - Maintain STM Working Buffer ("what fits in the window")
//   - Run Pipeline processors (masking, distillation, truncation)
//   - Render final []Message for LLM consumption (API or CLI)
//   - Cross-Agent bridge: publish/subscribe context packages
//
// Analogy: scratch paper, short-term memory, tactical whiteboard.
type ContextManager struct {
	sessionID   string
	topicID     string // Topic-scoped conversation ID
	agentID     string
	memory      *memory.MemoryContextManager
	pristine    *ContextGraph
	stm         *STMBuffer
	toolMgr     *tool.Manager
	pipeline    *PipelineOrchestrator
	bridge      *ContextBridge // Cross-Agent context handoff
	mode        string        // Current system mode (e.g., "balanced", "deep_think", "performance")
	provider    string        // Target LLM provider (e.g., "cli", "openai")
	workflowIDs []string

	mu        sync.RWMutex
	turnCount int32
}

// ContextManagerOption allows optional configuration.
type ContextManagerOption struct {
	SessionID string
	TopicID   string // Topic-scoped conversation ID
	AgentID   string
	ToolMgr   *tool.Manager
	Bridge    *ContextBridge // Cross-Agent bridge (nil = no bridging)
	Mode      string
	Provider  string
}

// NewContextManager creates a per-Agent ContextManager.
func NewContextManager(mem *memory.MemoryContextManager, opts ContextManagerOption) *ContextManager {
	if opts.AgentID == "" {
		opts.AgentID = "default"
	}

	cm := &ContextManager{
		sessionID: opts.SessionID,
		topicID:   opts.TopicID,
		agentID:   opts.AgentID,
		memory:    mem,
		pristine:  NewContextGraph(ScopeIsolated, opts.SessionID, opts.AgentID),
		stm:       NewSTMBuffer(DefaultSTMConfig()),
		toolMgr:   opts.ToolMgr,
		bridge:    opts.Bridge,
		mode:      opts.Mode,
		provider:  opts.Provider,
	}

	cm.pipeline = NewPipelineOrchestrator(cm.stm)
	cm.applyModeConfig()
	return cm
}

// applyModeConfig adjusts STM and pipeline parameters based on current mode.
func (cm *ContextManager) applyModeConfig() {
	switch cm.mode {
	case "deep_think":
		cm.stm.config.TokenBudget = 512000 // Full context
		cm.stm.config.ProtectedTurns = 10  // Keep more turns
		cm.pipeline.DisableAll()           // No compression
	case "performance":
		cm.stm.config.TokenBudget = 32000 // Aggressive budget
		cm.stm.config.ProtectedTurns = 1  // Only protect last turn
		cm.pipeline.EnableAll()
	case "survival":
		cm.stm.config.Capacity = 512
		cm.stm.config.TokenBudget = 8000
		cm.stm.config.ProtectedTurns = 1
		cm.pipeline.EnableAll()
	default: // balanced, casual, vigilant
		cm.stm.config.TokenBudget = 128000
		cm.stm.config.ProtectedTurns = 2
		cm.pipeline.EnableDefault()
	}
}

// ---------------------------------------------------------------------------
// Graph operations
// ---------------------------------------------------------------------------

// AppendNode adds a node to both Pristine Graph and STM.
func (cm *ContextManager) AppendNode(node *ConcreteNode) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	node.Version = int64(atomic.AddInt32(&cm.turnCount, 1))
	cm.pristine.AddNode(node)
	cm.stm.Append(node)

	// Root management: first node is always a root
	if len(cm.pristine.RootIDs) == 0 {
		cm.pristine.RootIDs = append(cm.pristine.RootIDs, node.ID)
	}
}

// AppendMessage creates and appends a node from a user or model message.
func (cm *ContextManager) AppendMessage(turnID string, role string, content string, nodeType NodeType) *ConcreteNode {
	node := &ConcreteNode{
		ID:        fmt.Sprintf("%s-%s-%s-%d", cm.agentID, role, turnID, cm.turnCount),
		Type:      nodeType,
		Role:      role,
		TurnID:    turnID,
		Timestamp: time.Now(),
		Content:   content,
		Metadata: map[string]string{
			MetaKeySource: "user",
			MetaKeyMode:   cm.mode,
		},
		TokenCount: estimateTokens(content),
	}
	cm.AppendNode(node)
	return node
}

// AppendToolResult creates a tool execution node.
// ---------------------------------------------------------------------------
// Assembly: Build the full context for LLM consumption
// ---------------------------------------------------------------------------

// AssembledContext holds the complete assembled context ready for rendering.
type AssembledContext struct {
	Tier1System string // Mode-aware system instruction
	Tier2Project string // Project memory context
	Tier3JIT     string // JIT-discovered file content
	History      []*ConcreteNode // STM sliding window (sorted)
	UserInput    string // Current user message
	TokenBudget  int    // Remaining budget after assembly
}

// Assemble builds the full context by combining MemoryContextManager (Tier 1-3)
// and ContextManager (STM history + current input).
func (cm *ContextManager) Assemble(ctx context.Context, userMessage string, workspace string, jitPaths []string, interceptionNote string, attachments []memory.AttachmentHandler) *AssembledContext {
	ac := &AssembledContext{
		TokenBudget: cm.stm.config.TokenBudget,
	}

	// Tier 1: System instruction
	ac.Tier1System = cm.memory.LoadSystemPrompt(ctx, cm.toolMgr, userMessage, attachments, interceptionNote)
	ac.TokenBudget -= estimateTokens(ac.Tier1System)

	// Tier 2: Project context
	ac.Tier2Project = cm.memory.LoadProjectContext()
	ac.TokenBudget -= estimateTokens(ac.Tier2Project)

	// Tier 3: JIT file discovery
	if len(jitPaths) > 0 {
		ac.Tier3JIT = cm.memory.JITDiscoverContext(ctx, workspace, jitPaths)
		ac.TokenBudget -= estimateTokens(ac.Tier3JIT)
	}

	// STM history (run pipeline first to compress)
	cm.pipeline.Run(ctx)
	ac.History = cm.stm.Snapshot()

	// Budget estimation for history
	for _, node := range ac.History {
		ac.TokenBudget -= node.TokenCount
	}

	// User input
	ac.UserInput = userMessage
	ac.TokenBudget -= estimateTokens(userMessage)

	return ac
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// Message represents a single message in the LLM prompt array.
type Message struct {
	Role    string `json:"role"`    // "system" | "user" | "assistant"
	Content string `json:"content"`
}

// RenderForAPI converts the AssembledContext into a []Message suitable
// for EINO or any structured API provider.
//
// ⚠️  Prompt Caching Strategy:
//   messages[0] (system) = Tier 1 + Tier 2 — PURE STATIC.
//   Static prefix MUST be byte-identical across all Agents and calls
//   so LLM provider KV cache hits at maximum depth.
//   Dynamic content (JIT, history, user input) goes AFTER the prefix.
func (cm *ContextManager) RenderForAPI(ac *AssembledContext) []Message {
	var messages []Message

	// [0] System instruction — STATIC ONLY (Tier 1 + Tier 2)
	// NEVER inject dynamic content here; it breaks prefix caching.
	sysParts := []string{ac.Tier1System}
	if ac.Tier2Project != "" {
		sysParts = append(sysParts, ac.Tier2Project)
	}
	systemContent := strings.TrimSpace(strings.Join(sysParts, "\n\n"))
	if systemContent != "" {
		messages = append(messages, Message{Role: "system", Content: systemContent})
	}

	// [1..n-1] STM History (semi-stable — identical if same conversation)
	var lastRole string
	for _, node := range ac.History {
		role := node.Role
		if role == "" {
			continue
		}
		if role == lastRole {
			messages[len(messages)-1].Content += "\n" + node.Content
			continue
		}
		messages = append(messages, Message{Role: role, Content: node.Content})
		lastRole = role
	}

	// [n] Current user input + JIT context (always dynamic — cache miss here)
	userContent := ac.UserInput
	if ac.Tier3JIT != "" {
		// JIT attachments go into the user message (NOT system) so the
		// static prefix above remains cacheable across queries.
		userContent = "Context:\n" + ac.Tier3JIT + "\n\n" + userContent
	}
	messages = append(messages, Message{Role: "user", Content: userContent})

	return messages
}

// RenderForCLI flattens the AssembledContext into a single prompt string
// for CLI providers that accept only flat text (e.g., agy, copilot, gemini-cli).
//
// Same caching strategy as RenderForAPI: static prefix first, dynamic last.
func (cm *ContextManager) RenderForCLI(ac *AssembledContext) string {
	var parts []string

	// [SYSTEM] — STATIC ONLY (Tier 1 + Tier 2). JIT goes into [USER].
	sysParts := []string{ac.Tier1System}
	if ac.Tier2Project != "" {
		sysParts = append(sysParts, ac.Tier2Project)
	}
	systemContent := strings.TrimSpace(strings.Join(sysParts, "\n\n"))
	if systemContent != "" {
		parts = append(parts, "[SYSTEM]\n"+systemContent+"\n[/SYSTEM]")
	}

	// History
	for _, node := range ac.History {
		label := strings.ToUpper(node.Role)
		if node.Role == "model" {
			label = "ASSISTANT"
		}
		parts = append(parts, fmt.Sprintf("[%s]\n%s\n[/%s]", label, node.Content, label))
	}

	// Current user input + JIT (always dynamic)
	userContent := ac.UserInput
	if ac.Tier3JIT != "" {
		userContent = "Context:\n" + ac.Tier3JIT + "\n\n" + userContent
	}
	parts = append(parts, "[USER]\n"+userContent+"\n[/USER]")

	return strings.Join(parts, "\n\n")
}

// ---------------------------------------------------------------------------
// Vertical Context Stripping — 逐层剥离
//
// 大脑 (Cerebrum):      全量 → 完整的 AssembledContext (sys + 记忆 + STM + 工具)
// 小脑 (Cerebellum):    精简 → 只拿意图/指令/工具 schemas，看不到完整历史
// 间脑 (Diencephalon):  最小 → 只拿渲染好的 []Message + provider config
// 脑干 (Brainstem):      零上下文 → 只拿裸命令 "git commit -m 'x'"
//
// 核心原则：下级只能看到执行当前步骤"必须知道"的信息。
// ---------------------------------------------------------------------------

// CerebellumInstruction is the stripped context for the Cerebellum.
// It carries only what's needed for tactical execution — no full history.
type CerebellumInstruction struct {
	SessionID   string
	TopicID     string
	Intent      string            // High-level intent ("修复 auth 401 错误")
	ToolSchemas []string          // Available tool definitions (names + signatures)
	CurrentStep string            // The specific step to execute now
	Artifacts   map[string]string // Relevant artifacts only (e.g. {"error_log": "..."})
	TokenBudget int
}

// RenderForCerebellum strips the full context down to what the Cerebellum needs.
// The Cerebellum does NOT see:
//   - Full conversation history / STM (it only gets the current step)
//   - Tier 2 project memory (it doesn't need project-level background)
//   - Cross-agent bridge packages (those belong to the Brain layer)
//   - JIT file content (it gets only the relevant artifact excerpts)
func (cm *ContextManager) RenderForCerebellum(ac *AssembledContext, intent, currentStep string) *CerebellumInstruction {
	// Tool schemas from the tool manager
	var schemas []string
	if cm.toolMgr != nil {
		for _, t := range cm.toolMgr.List() {
			schemas = append(schemas, t.Name+": "+t.Description)
		}
	}

	// Relevant artifacts: only the user's current input and the step instruction
	artifacts := make(map[string]string)
	artifacts["user_input"] = ac.UserInput
	if ac.Tier3JIT != "" {
		artifacts["jit_context"] = truncateStr(ac.Tier3JIT, 500) // 只给摘要！
	}

	return &CerebellumInstruction{
		SessionID:   cm.sessionID,
		TopicID:     cm.topicID,
		Intent:      intent,
		ToolSchemas: schemas,
		CurrentStep: currentStep,
		Artifacts:   artifacts,
		TokenBudget: ac.TokenBudget,
	}
}

// RenderForDiencephalon strips down to the absolute minimum — just the
// assembled messages ready for LLM API call.
//
// Diencephalon only sees:
//   - []Message (system + user + assistant turns) — the final rendered prompt
//   - Provider name (which LLM to call)
//   - Cache hints for response caching
//
// Diencephalon does NOT see:
//   - The AssembledContext structure (Tier breakdown, budget, JIT paths)
//   - Tool schemas or definitions (those were already baked into the system prompt)
//   - Pristine Graph — it has no graph awareness at all
//   - Cross-agent bridge — it's just a relay
type DiencephalonPayload struct {
	Messages    []Message // The final prompt ready for the LLM
	Provider    string    // "openai" | "deepseek" | "gemini" | "cli"
	Model       string    // Specific model name
	Tools       []string  // Tool definitions already serialized (for API providers)
	SessionID   string
	TopicID     string
}

// RenderForDiencephalon produces the minimal payload for the LLM relay.
func (cm *ContextManager) RenderForDiencephalon(ac *AssembledContext) *DiencephalonPayload {
	messages := cm.RenderForAPI(ac)

	return &DiencephalonPayload{
		Messages:  messages,
		Provider:  cm.provider,
		Model:     "", // Set by the caller if known
		SessionID: cm.sessionID,
		TopicID:   cm.topicID,
	}
}

// ---------------------------------------------------------------------------
// Cross-Agent Bridge: Publish & Import
// ---------------------------------------------------------------------------

// PublishContext creates a context package from the current Agent's state
// and publishes it to the bridge for other Agents to consume.
//
// Example: RefundAgent finishes analysis and publishes to "analysis-result"
//
//	cm.PublishContext("analysis-result", "Fraud detected, confidence 0.95", map[string]string{
//	    "risk_level": "high",
//	})
func (cm *ContextManager) PublishContext(label, summary string, artifacts map[string]string) string {
	if cm.bridge == nil {
		return ""
	}

	pkg := NewContextPackage(cm.agentID, label, summary)
	for k, v := range artifacts {
		pkg.Artifacts[k] = v
	}
	pkg.SessionID = cm.sessionID
	pkg.TopicID = cm.topicID
	pkg.TargetAgent = "" // broadcast within scope

	channel := BridgeChannel{
		Name:      ChannelName(ScopeLocal, cm.sessionID, cm.topicID, label),
		Scope:     ScopeLocal,
		SessionID: cm.sessionID,
		TopicID:   cm.topicID,
		Label:     label,
	}
	return cm.bridge.Publish(channel, pkg)
}

// PublishDirect sends a package specifically to one Agent.
func (cm *ContextManager) PublishDirect(targetAgentID, summary string, artifacts map[string]string) string {
	if cm.bridge == nil {
		return ""
	}
	pkg := NewContextPackage(cm.agentID, "direct", summary)
	for k, v := range artifacts {
		pkg.Artifacts[k] = v
	}
	pkg.SessionID = cm.sessionID
	return cm.bridge.PublishDirect(targetAgentID, pkg)
}

// ImportContext pulls packages from subscribed channels and injects them
// into the STM as system_event nodes. This is how Agent B learns what
// Agent A found out — without seeing A's Pristine Graph.
//
// Returns the number of packages imported.
func (cm *ContextManager) ImportContext(ctx context.Context) int {
	if cm.bridge == nil {
		return 0
	}

	channels := cm.bridge.SubscribedChannels(cm.agentID)
	imported := 0

	for _, chName := range channels {
		pkgs := cm.bridge.ReadChannel(cm.agentID, chName)
		for _, pkg := range pkgs {
			content := fmt.Sprintf("[Bridge:%s←%s] %s",
				pkg.Label, pkg.SourceAgent, pkg.Summary)
			if len(pkg.Artifacts) > 0 {
				for k, v := range pkg.Artifacts {
					content += fmt.Sprintf("\n  %s: %s", k, v)
				}
			}

			node := &ConcreteNode{
				ID:        fmt.Sprintf("bridge-%s-%s-%s", pkg.SourceAgent, pkg.Label, pkg.ID),
				Type:      NodeSystemEvent,
				Role:      "model",
				TurnID:    fmt.Sprintf("bridge-%d", pkg.Timestamp.UnixNano()),
				Timestamp: pkg.Timestamp,
				Content:   content,
				Metadata: map[string]string{
					MetaKeySource:   "bridge",
					MetaKeyProvider: pkg.SourceAgent,
				},
				TokenCount: estimateTokens(content),
			}
			cm.AppendNode(node)
			imported++
		}
	}

	return imported
}

// SubscribeToChannel subscribes to a bridge channel by label.
// Shortcut: subscribes within the current session/topic scope.
func (cm *ContextManager) SubscribeToChannel(label string) {
	if cm.bridge == nil {
		return
	}
	chName := ChannelName(ScopeLocal, cm.sessionID, cm.topicID, label)
	cm.bridge.Subscribe(cm.agentID, chName)

	// Auto-grant access (local scope is open within the topic)
	cm.bridge.GrantAccess(cm.agentID, chName)
}

// Bridge returns the cross-Agent bridge instance (for orchestration code).
func (cm *ContextManager) Bridge() *ContextBridge {
	return cm.bridge
}

// Override AppendToolResult to also write to cache for cross-Agent sharing.
func (cm *ContextManager) AppendToolResult(turnID string, toolName string, input string, output string) *ConcreteNode {
	node := &ConcreteNode{
		ID:        fmt.Sprintf("%s-tool-%s-%s", cm.agentID, toolName, turnID),
		Type:      NodeToolResult,
		Role:      "model", // Tool calls are model-side
		TurnID:    turnID,
		Timestamp: time.Now(),
		Content:   fmt.Sprintf("Tool(%s): %s\n→ %s", toolName, truncateStr(input, 500), truncateStr(output, 2000)),
		Metadata: map[string]string{
			MetaKeySource:   "tool",
			MetaKeyProvider: toolName,
			MetaKeyMode:     cm.mode,
		},
		TokenCount: estimateTokens(output),
	}
	cm.AppendNode(node)

	// Also write to session-wide tool cache if available
	if cm.memory != nil && cm.memory.Cache() != nil {
		cache := cm.memory.Cache()
		// argHash is derived from input, toolName is the key
		cache.SetSessionToolResult(cm.sessionID, toolName, input, output)
	}

	return node
}

// ---------------------------------------------------------------------------
// Persistence
// ---------------------------------------------------------------------------

// Persist saves the Pristine Graph to durable storage.
// Designed for batch write at end of turn (Dapr Actor / Redis L2 pattern).
func (cm *ContextManager) Persist(ctx context.Context) error {
	// TODO: Implement with Dapr State Store / Redis
	// For now, the graph remains in-memory
	cm.mu.Lock()
	cm.pristine.UpdatedAt = time.Now()
	cm.mu.Unlock()
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// estimateTokens is a rough token estimator (4 chars per token).
func estimateTokens(s string) int {
	return len([]rune(s)) / 4
}

func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// Ensure tool.Manager compatibility.
// We use the *tool.Manager type directly from the existing import.
