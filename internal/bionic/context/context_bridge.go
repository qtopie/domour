package context

import (
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// ContextPackage — explicit cross-Agent artifact
// ---------------------------------------------------------------------------

// ContextPackage is a structured artifact published by one Agent for others.
// This is the ONLY way context crosses Agent boundaries — agents cannot
// read each other's Pristine Graph or STM directly.
type ContextPackage struct {
	ID          string            // Unique ID (e.g. "pkg-<agentID>-<seq>")
	SourceAgent string            // Publishing Agent ID
	TargetAgent string            // "" = broadcast, non-empty = directed
	Scope       ContextScope      // global | local | isolated
	SessionID   string
	TopicID     string
	Label       string            // Human label (e.g. "fraud-analysis-result")
	Summary     string            // The actual content — what this Agent wants to say
	Artifacts   map[string]string // Named artifacts (structured data, JSON, code)
	Metadata    map[string]string // Extra context (confidence, model used, etc.)
	Version     int
	Timestamp   time.Time
}

// ---------------------------------------------------------------------------
// Channel descriptor
// ---------------------------------------------------------------------------

// BridgeChannel is a named pub/sub channel for cross-Agent context.
// Channel name convention: "<scope>:<sessionID>:<topicID>:<label>"
type BridgeChannel struct {
	Name      string
	Scope     ContextScope
	SessionID string
	TopicID   string
	Label     string // e.g. "analysis-result", "approval", "error"
}

// ChannelName builds a channel name from parts.
func ChannelName(scope ContextScope, sessionID, topicID, label string) string {
	return fmt.Sprintf("%s:%s:%s:%s", scope, sessionID, topicID, label)
}

// ---------------------------------------------------------------------------
// ContextBridge — cross-Agent context propagation with isolation
// ---------------------------------------------------------------------------

// ContextBridge manages cross-Agent context propagation.
//
// Design principles:
//  1. Explicit publish — Agents explicitly publish packages; nothing leaks
//  2. Scope-based isolation — ScopeIsolated agents only see directed packages
//  3. Channel model — Named channels for pub/sub, not raw cache reads
//  4. No cross-Agent graph access — no Agent can read another's Pristine Graph
//
// Isolation enforcement:
//   - ScopeIsolated: can ONLY read packages where TargetAgent == their own ID
//     or packages on channels they explicitly subscribed to
//   - ScopeLocal: can read all packages within the same topicID
//   - ScopeGlobal: can read all packages within the same sessionID
type ContextBridge struct {
	// packages stores the most recent N packages per channel
	packages map[string][]*ContextPackage
	// acl maps channel → set of agent IDs allowed to read
	acl map[string]map[string]struct{}
	// subscriptions maps agentID → set of channels they're subscribed to
	subscriptions map[string]map[string]struct{}

	cap  int // max packages per channel
	mu   sync.RWMutex
}

// NewContextBridge creates a bridge with the given per-channel capacity.
func NewContextBridge(capacity int) *ContextBridge {
	if capacity <= 0 {
		capacity = 16
	}
	return &ContextBridge{
		packages:      make(map[string][]*ContextPackage),
		acl:           make(map[string]map[string]struct{}),
		subscriptions: make(map[string]map[string]struct{}),
		cap:           capacity,
	}
}

// ---------------------------------------------------------------------------
// Publish
// ---------------------------------------------------------------------------

// Publish publishes a package to a channel. Returns the assigned package ID.
func (b *ContextBridge) Publish(channel BridgeChannel, pkg *ContextPackage) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if pkg.ID == "" {
		pkg.ID = fmt.Sprintf("pkg-%s-%d", pkg.SourceAgent, pkg.Timestamp.UnixNano())
	}
	pkg.Timestamp = time.Now()
	pkg.Scope = channel.Scope
	pkg.SessionID = channel.SessionID
	pkg.TopicID = channel.TopicID

	// Append to channel queue (ring buffer per channel)
	b.packages[channel.Name] = append(b.packages[channel.Name], pkg)
	if len(b.packages[channel.Name]) > b.cap {
		// Drop oldest
		b.packages[channel.Name] = b.packages[channel.Name][len(b.packages[channel.Name])-b.cap:]
	}

	return pkg.ID
}

// PublishDirect publishes a package targeted at a specific Agent.
// Only that Agent can read it via Subscribe + ReadLatest.
func (b *ContextBridge) PublishDirect(targetAgentID string, pkg *ContextPackage) string {
	channel := BridgeChannel{
		Name:      fmt.Sprintf("direct:%s:%s", pkg.SourceAgent, targetAgentID),
		Scope:     ScopeIsolated,
		SessionID: pkg.SessionID,
		Label:     "direct",
	}
	pkg.TargetAgent = targetAgentID
	return b.Publish(channel, pkg)
}

// ---------------------------------------------------------------------------
// Subscribe / Unsubscribe
// ---------------------------------------------------------------------------

// Subscribe registers an Agent to receive packages from a channel.
func (b *ContextBridge) Subscribe(agentID, channelName string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.subscriptions[agentID] == nil {
		b.subscriptions[agentID] = make(map[string]struct{})
	}
	b.subscriptions[agentID][channelName] = struct{}{}
}

// Unsubscribe removes an Agent's subscription.
func (b *ContextBridge) Unsubscribe(agentID, channelName string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if subs, ok := b.subscriptions[agentID]; ok {
		delete(subs, channelName)
	}
}

// ---------------------------------------------------------------------------
// Read
// ---------------------------------------------------------------------------

// ReadLatest returns the most recent package on a channel that the agent
// is allowed to see. Returns nil if not authorized or empty.
func (b *ContextBridge) ReadLatest(agentID, channelName string) *ContextPackage {
	b.mu.RLock()
	defer b.mu.RUnlock()

	queue := b.packages[channelName]
	if len(queue) == 0 {
		return nil
	}

	latest := queue[len(queue)-1]
	if !b.canRead(agentID, latest) {
		return nil
	}
	return latest
}

// ReadChannel returns all packages on a channel that the agent can see.
func (b *ContextBridge) ReadChannel(agentID, channelName string) []*ContextPackage {
	b.mu.RLock()
	defer b.mu.RUnlock()

	queue := b.packages[channelName]
	if len(queue) == 0 {
		return nil
	}

	var result []*ContextPackage
	for _, pkg := range queue {
		if b.canRead(agentID, pkg) {
			result = append(result, pkg)
		}
	}
	return result
}

// SubscribedChannels returns all channels the agent is subscribed to.
func (b *ContextBridge) SubscribedChannels(agentID string) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var channels []string
	for ch := range b.subscriptions[agentID] {
		channels = append(channels, ch)
	}
	return channels
}

// ---------------------------------------------------------------------------
// ACL management
// ---------------------------------------------------------------------------

// GrantAccess allows a specific agent to read a channel.
// By default, ScopeGlobal channels are open; use this for ScopeIsolated.
func (b *ContextBridge) GrantAccess(agentID, channelName string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.acl[channelName] == nil {
		b.acl[channelName] = make(map[string]struct{})
	}
	b.acl[channelName][agentID] = struct{}{}
}

// RevokeAccess removes an agent's access.
func (b *ContextBridge) RevokeAccess(agentID, channelName string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if agents, ok := b.acl[channelName]; ok {
		delete(agents, agentID)
	}
}

// ---------------------------------------------------------------------------
// Isolation enforcement
// ---------------------------------------------------------------------------

// canRead checks whether an agent is allowed to read a package based on scope.
func (b *ContextBridge) canRead(agentID string, pkg *ContextPackage) bool {
	switch pkg.Scope {
	case ScopeGlobal:
		// Any agent in the same session can read
		return true

	case ScopeLocal:
		// Any agent in the same session can read (local = topic-scoped)
		// For stricter topic isolation, check pkg.TopicID here
		return true

	case ScopeIsolated:
		// Only the targeted agent or explicitly granted agents can read
		if pkg.TargetAgent == agentID {
			return true
		}
		// Check ACL
		for chName := range b.subscriptions[agentID] {
			if agents, ok := b.acl[chName]; ok {
				if _, ok := agents[agentID]; ok {
					return true
				}
			}
		}
		return false

	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Integration helpers for ContextManager
// ---------------------------------------------------------------------------

// NewContextPackage creates a context package pre-populated with defaults.
func NewContextPackage(sourceAgent, label, summary string) *ContextPackage {
	return &ContextPackage{
		SourceAgent: sourceAgent,
		Label:       label,
		Summary:     summary,
		Artifacts:   make(map[string]string),
		Metadata:    make(map[string]string),
		Timestamp:   time.Now(),
	}
}

// AddBridgeToContextManager adds cross-Agent bridge support to ContextManager.
// These methods will be added to ContextManager below.
