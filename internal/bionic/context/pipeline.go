package context

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Pipeline Orchestrator
// ---------------------------------------------------------------------------

// PipelineProcessor is a single stage in the context compression pipeline.
type PipelineProcessor interface {
	// Name returns the processor identifier.
	Name() string
	// Process runs the processor on the STM buffer. Returns modified nodes.
	Process(ctx context.Context, nodes []*ConcreteNode) ([]*ConcreteNode, error)
}

// PipelineOrchestrator manages the ordered chain of compression processors.
// Each processor has a skip flag that the Mode scheduler can toggle.
type PipelineOrchestrator struct {
	stm        *STMBuffer
	processors []pipelineStage
	mu         sync.RWMutex
}

// pipelineStage pairs a processor with its enable flag.
type pipelineStage struct {
	processor PipelineProcessor
	enabled   bool
}

// NewPipelineOrchestrator creates the pipeline with default processor chain.
func NewPipelineOrchestrator(stm *STMBuffer) *PipelineOrchestrator {
	po := &PipelineOrchestrator{
		stm: stm,
	}
	po.registerDefaults()
	return po
}

// registerDefaults adds the standard processor chain in execution order.
func (po *PipelineOrchestrator) registerDefaults() {
	po.processors = []pipelineStage{
		{processor: &ToolMaskingProcessor{}, enabled: true},
		{processor: &BlobDegradationProcessor{}, enabled: true},
		{processor: &NodeDistillationProcessor{}, enabled: true},
		{processor: &NodeTruncationProcessor{}, enabled: true},
	}
}

// DisableAll disables all processors.
func (po *PipelineOrchestrator) DisableAll() {
	po.mu.Lock()
	defer po.mu.Unlock()
	for i := range po.processors {
		po.processors[i].enabled = false
	}
}

// EnableAll enables all processors.
func (po *PipelineOrchestrator) EnableAll() {
	po.mu.Lock()
	defer po.mu.Unlock()
	for i := range po.processors {
		po.processors[i].enabled = true
	}
}

// EnableDefault enables the default processor set (same as registerDefaults).
func (po *PipelineOrchestrator) EnableDefault() {
	po.mu.Lock()
	defer po.mu.Unlock()
	po.processors = nil
	po.registerDefaults()
}

// Run executes all enabled processors in order.
func (po *PipelineOrchestrator) Run(ctx context.Context) error {
	po.mu.RLock()
	defer po.mu.RUnlock()

	nodes := po.stm.Snapshot()
	protectedIdx := po.stm.ProtectedBoundary()

	for _, stage := range po.processors {
		if !stage.enabled {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Only process the compressible portion (before protected boundary)
		if protectedIdx > 0 && len(nodes) > protectedIdx {
			compressible := nodes[:protectedIdx]
			processed, err := stage.processor.Process(ctx, compressible)
			if err != nil {
				continue // Skip processor on failure, don't halt pipeline
			}
			// Merge: keep protected zone unchanged, replace compressible
			nodes = append(processed, nodes[protectedIdx:]...)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// ToolMaskingProcessor
// ---------------------------------------------------------------------------

// ToolMaskingProcessor masks or replaces oversized tool outputs.
// Applies when a tool result node exceeds the masking threshold (8K tokens).
type ToolMaskingProcessor struct {
	MaxTokenCount int
}

const defaultToolMaskingThreshold = 8000

func (p *ToolMaskingProcessor) Name() string {
	return "ToolMasking"
}

func (p *ToolMaskingProcessor) Process(ctx context.Context, nodes []*ConcreteNode) ([]*ConcreteNode, error) {
	threshold := p.MaxTokenCount
	if threshold <= 0 {
		threshold = defaultToolMaskingThreshold
	}

	results := make([]*ConcreteNode, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if node.Type == NodeToolResult && node.TokenCount > threshold {
			masked := &ConcreteNode{
				ID:         node.ID + "-masked",
				Type:       NodeToolResult,
				Role:       node.Role,
				TurnID:     node.TurnID,
				Timestamp:  node.Timestamp,
				Content:    truncateToolOutput(node.Content, 2000),
				Metadata:   copyMap(node.Metadata),
				TokenCount: estimateTokens(truncateToolOutput(node.Content, 2000)),
				ReplacesID: node.ID,
			}
			if masked.Metadata == nil {
				masked.Metadata = make(map[string]string)
			}
			masked.Metadata["masked"] = "true"
			masked.Metadata["original_token_count"] = fmt.Sprintf("%d", node.TokenCount)
			results = append(results, masked)
		} else {
			results = append(results, node)
		}
	}
	return results, nil
}

// truncateToolOutput shortens tool output by keeping a summary pattern.
func truncateToolOutput(content string, maxChars int) string {
	// Try to find the first meaningful content line
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) == 0 {
		return "[empty tool result]"
	}

	var out []string
	total := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		runes := []rune(trimmed)
		if total+len(runes) > maxChars {
			remaining := maxChars - total
			if remaining > 20 {
				out = append(out, string(runes[:remaining]))
			}
			break
		}
		out = append(out, trimmed)
		total += len(runes)
	}

	result := strings.Join(out, "\n")
	if total < len([]rune(content)) {
		result += "\n...[truncated]"
	}
	return result
}

// ---------------------------------------------------------------------------
// BlobDegradationProcessor
// ---------------------------------------------------------------------------

// BlobDegradationProcessor replaces large text blobs with shorter summaries
// by stripping redundant content (e.g., repeated log lines).
type BlobDegradationProcessor struct {
	MaxBlobTokenCount int
}

const defaultBlobDegradationThreshold = 15000

func (p *BlobDegradationProcessor) Name() string {
	return "BlobDegradation"
}

func (p *BlobDegradationProcessor) Process(ctx context.Context, nodes []*ConcreteNode) ([]*ConcreteNode, error) {
	threshold := p.MaxBlobTokenCount
	if threshold <= 0 {
		threshold = defaultBlobDegradationThreshold
	}

	results := make([]*ConcreteNode, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if node.TokenCount > threshold && node.Role == "model" {
			// Degrade: replace content with a summary marker
			degraded := &ConcreteNode{
				ID:         node.ID + "-degraded",
				Type:       node.Type,
				Role:       node.Role,
				TurnID:     node.TurnID,
				Timestamp:  node.Timestamp,
				Content:    fmt.Sprintf("[degraded: original %d tokens; replaced with brief summary]", node.TokenCount),
				Metadata:   copyMap(node.Metadata),
				TokenCount: 20,
				ReplacesID: node.ID,
			}
			if degraded.Metadata == nil {
				degraded.Metadata = make(map[string]string)
			}
			degraded.Metadata["degraded"] = "true"
			results = append(results, degraded)
		} else {
			results = append(results, node)
		}
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// NodeDistillationProcessor
// ---------------------------------------------------------------------------

// NodeDistillationProcessor compresses full turns into rolling summaries.
// It groups consecutive nodes by TurnID and replaces them with concise text.
type NodeDistillationProcessor struct {
	MinTurnTokens int // Min total tokens in a turn to trigger distillation
}

const defaultDistillationMinTokens = 15000

func (p *NodeDistillationProcessor) Name() string {
	return "NodeDistillation"
}

func (p *NodeDistillationProcessor) Process(ctx context.Context, nodes []*ConcreteNode) ([]*ConcreteNode, error) {
	threshold := p.MinTurnTokens
	if threshold <= 0 {
		threshold = defaultDistillationMinTokens
	}

	// Group by TurnID
	turns := make(map[string][]*ConcreteNode)
	turnOrder := make([]string, 0)
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if _, ok := turns[node.TurnID]; !ok {
			turnOrder = append(turnOrder, node.TurnID)
		}
		turns[node.TurnID] = append(turns[node.TurnID], node)
	}

	results := make([]*ConcreteNode, 0, len(turnOrder))
	for _, tid := range turnOrder {
		turnNodes := turns[tid]
		totalTokens := 0
		for _, n := range turnNodes {
			totalTokens += n.TokenCount
		}

		if totalTokens > threshold {
			// Distill: create a single summary node
			userParts := make([]string, 0)
			modelParts := make([]string, 0)
			for _, n := range turnNodes {
				if n.Role == "user" {
					userParts = append(userParts, n.Content)
				} else {
					modelParts = append(modelParts, n.Content)
				}
			}
			summary := fmt.Sprintf("[Distilled turn %s: user: %s | model: %s]",
				tid, summarizeText(strings.Join(userParts, " | "), 200),
				summarizeText(strings.Join(modelParts, " | "), 500))

			distilled := &ConcreteNode{
				ID:           fmt.Sprintf("distill-%s", tid),
				Type:         NodeRollingSummary,
				Role:         "model",
				TurnID:       tid,
				Content:      summary,
				TokenCount:   estimateTokens(summary),
				AbstractsIDs: nodeIDs(turnNodes),
			}
			results = append(results, distilled)
		} else {
			results = append(results, turnNodes...)
		}
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// NodeTruncationProcessor
// ---------------------------------------------------------------------------

// NodeTruncationProcessor drops old turns when the node count exceeds the STM capacity.
// The most recent turns are always preserved.
type NodeTruncationProcessor struct {
	MaxNodes int
}

const defaultTruncationMaxNodes = 4000

func (p *NodeTruncationProcessor) Name() string {
	return "NodeTruncation"
}

func (p *NodeTruncationProcessor) Process(ctx context.Context, nodes []*ConcreteNode) ([]*ConcreteNode, error) {
	maxNodes := p.MaxNodes
	if maxNodes <= 0 {
		maxNodes = defaultTruncationMaxNodes
	}

	if len(nodes) <= maxNodes {
		return nodes, nil
	}

	// Keep the last maxNodes
	truncated := nodes[len(nodes)-maxNodes:]
	return truncated, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func copyMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func nodeIDs(nodes []*ConcreteNode) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}

func summarizeText(text string, maxLen int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxLen {
		return string(runes)
	}
	return string(runes[:maxLen]) + "..."
}
