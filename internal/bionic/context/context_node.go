// Package context defines the core data types for Domour's dual-layer
// context system: MemoryContextManager (global/shared) and ContextManager
// (per-Agent/session).
package context

import (
	"fmt"
	"time"
)

// NodeType represents the semantic type of a context node.
type NodeType string

const (
	NodeUserPrompt     NodeType = "user_prompt"
	NodeAgentThought   NodeType = "agent_thought"
	NodeToolCall       NodeType = "tool_call"
	NodeToolResult     NodeType = "tool_result"
	NodeSystemEvent    NodeType = "system_event"
	NodeIntent         NodeType = "intent"
	NodeSnapshot       NodeType = "snapshot"
	NodeRollingSummary NodeType = "rolling_summary"
)

// ConcreteNode is the smallest atomic unit in the context graph.
// It wraps LLM Provider-native Content objects bidirectionally.
type ConcreteNode struct {
	ID           string            // Stable hash ID (Content + Type + TurnID)
	Type         NodeType          // Node type
	Role         string            // Only "user" | "model"
	TurnID       string            // Belongs to which turn
	Timestamp    time.Time         // Creation time
	Content      string            // Text content (simplified; full Payload via Diencephalon)
	Metadata     map[string]string // Source, token count, security level, etc.
	ReplacesID   string            // 1:1 replacement chain (e.g., ToolMasking)
	AbstractsIDs []string          // N:1 summary chain
	TokenCount   int               // Estimated token count
	Version      int64             // Optimistic lock version
}

// ContextScope defines the visibility boundary of a context graph.
type ContextScope string

const (
	ScopeGlobal   ContextScope = "global"
	ScopeLocal    ContextScope = "local"
	ScopeIsolated ContextScope = "isolated"
)

// ContextGraph is a DAG representation of context nodes.
type ContextGraph struct {
	Nodes     map[string]*ConcreteNode // nodeID → Node
	Edges     map[string][]string      // parentID → childIDs
	RootIDs   []string                 // Root node IDs (heads)
	Scope     ContextScope
	SessionID string
	AgentID   string
	Version   int64     // Monotonic version
	UpdatedAt time.Time
}

// NewContextGraph creates an empty graph with the given scope.
func NewContextGraph(scope ContextScope, sessionID, agentID string) *ContextGraph {
	return &ContextGraph{
		Nodes:     make(map[string]*ConcreteNode),
		Edges:     make(map[string][]string),
		Scope:     scope,
		SessionID: sessionID,
		AgentID:   agentID,
		Version:   1,
		UpdatedAt: time.Now(),
	}
}

// AddNode inserts a node into the graph.
func (g *ContextGraph) AddNode(node *ConcreteNode) {
	g.Nodes[node.ID] = node
	g.Version++
	g.UpdatedAt = time.Now()
}

// AddEdge connects parent → child.
func (g *ContextGraph) AddEdge(parentID, childID string) error {
	if _, ok := g.Nodes[parentID]; !ok {
		return fmt.Errorf("parent node %s not found", parentID)
	}
	if _, ok := g.Nodes[childID]; !ok {
		return fmt.Errorf("child node %s not found", childID)
	}
	g.Edges[parentID] = append(g.Edges[parentID], childID)
	return nil
}

// RemoveNode removes a node and cascading edges.
func (g *ContextGraph) RemoveNode(nodeID string) {
	delete(g.Nodes, nodeID)
	delete(g.Edges, nodeID)
	for parent, children := range g.Edges {
		filtered := make([]string, 0, len(children))
		for _, child := range children {
			if child != nodeID {
				filtered = append(filtered, child)
			}
		}
		g.Edges[parent] = filtered
	}
}

// SnapshotMetadata keys used in ConcreteNode.Metadata.
const (
	MetaKeySecurityLevel = "security_level" // "normal" | "sensitive" | "critical"
	MetaKeySource        = "source"         // "user" | "system" | "tool"
	MetaKeyMode          = "mode"           // Current system mode when node was created
	MetaKeyProvider      = "provider"       // LLM provider that generated this node
	MetaKeyIntent        = "intent"         // Intent label
)

// SecurityLevel constants for MetaKeySecurityLevel.
const (
	SecurityNormal    = "normal"
	SecuritySensitive = "sensitive"
	SecurityCritical  = "critical"
)
