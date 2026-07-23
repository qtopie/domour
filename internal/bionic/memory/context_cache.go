package memory

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/qtopie/domour/ark/infra/cache"
)

// ---------------------------------------------------------------------------
// Cache Key Hierarchy
// ---------------------------------------------------------------------------
//
// 作用域层级:  Global > Project > Session > Conversation > Agent
//
// 层级                | Key 格式                                          | 作用域         | Cache   | 示例
// --------------------+--------------------------------------------------+----------------+---------+-----------------------
// Tier 1 (全局规则)    | tier1:global                                      | 全局           | long    | ~/.domour/*.md
// Tier 1 (技能提示词)  | tier1:skills:<name>                               | 全局           | long    | skills:bash
// Tier 2 (项目记忆)    | tier2:project:<hash(dir)>                         | 项目           | long    | <project>/.domour/*.md
// Tier 3 (JIT 文件)   | tier3:file:<hash(path)>                           | 文件路径       | short   | workspace 文件内容
// Session 元数据      | sess:meta:<sessionID>                             | 会话           | long    | provider/model/config
// Session 工具结果     | sess:tool:<sessionID>:<toolName>:<argHash[:12]>    | 会话共享结果   | short   | 任意 tool 的输出
// Topic 元数据        | topic:meta:<sessionID>:<topicID>                   | 会话内对话     | long    | topic/创建时间
// Topic STM           | topic:stm:<sessionID>:<topicID>                    | 会话内对话     | short   | STM 缓冲区快照
// Agent 组装结果      | ctx:assemble:<sessionID>:<topicID>:<agentID>:<seq> | Agent 独立     | short   | AssembledContext
// Agent 渲染结果      | ctx:render:<sessionID>:<topicID>:<agentID>:<seq>T  | Agent 独立     | short   | []Message / flat string

// CachePolicy defines TTL and invalidation behavior for a cache entry.
type CachePolicy struct {
	TTL        time.Duration
	AutoPurge  bool // Whether to auto-purge on session end
	SizeBudget int  // Max entries for this tier (0 = unlimited)
}

// Default cache policies per tier.
var (
	Tier1Policy = CachePolicy{TTL: 30 * time.Minute, AutoPurge: false, SizeBudget: 32}
	Tier2Policy = CachePolicy{TTL: 30 * time.Minute, AutoPurge: false, SizeBudget: 8}
	Tier3Policy = CachePolicy{TTL: 5 * time.Minute, AutoPurge: false, SizeBudget: 256}
	CtxAssemble = CachePolicy{TTL: 5 * time.Second, AutoPurge: true, SizeBudget: 1024}
	CtxRender   = CachePolicy{TTL: 5 * time.Second, AutoPurge: true, SizeBudget: 1024}
)

// ContextCache is a shared L1 cache for the memory subsystem.
// It uses TWO separate Otter instances for different TTL tiers:
//
//   - longCache (30min TTL): Tier 1 (global rules, skills), Tier 2 (project mem)
//     → Shared across all Agent instances; evicted only on project change or TTL
//
//   - shortCache (5min TTL): Tier 3 (JIT files), ctx:assemble, ctx:render
//     → Per-Agent session keys expire quickly to keep cache fresh
//
// Design principles:
//   - Tier 1/2 entries are keyed by content hash → every Agent hits the same cache line
//   - ctx:* entries are keyed by session+agent → naturally isolated, short-lived
//   - Otter handles automatic TTL expiration for both caches independently
type ContextCache struct {
	longCache  *cache.Cache[string, string] // 30min TTL: Tier 1 + Tier 2
	shortCache *cache.Cache[string, string] // 5min TTL:  Tier 3 + ctx:*
	mu         sync.RWMutex
	stats      CacheStats
}

// CacheStats tracks hit/miss ratios for observability.
type CacheStats struct {
	Hits   int64
	Misses int64
	Size   int
}

// NewContextCache creates a dual-ttl L1 cache.
func NewContextCache(capacity int) *ContextCache {
	if capacity <= 0 {
		capacity = 2048
	}

	// Split budget: ~60% for long-lived, ~40% for short-lived
	longBudget := capacity * 6 / 10
	if longBudget < 64 {
		longBudget = 64
	}
	shortBudget := capacity - longBudget
	if shortBudget < 64 {
		shortBudget = 64
	}

	longCache, err := cache.NewCache[string, string](longBudget, 30*time.Minute)
	if err != nil {
		longCache = nil
	}
	shortCache, err := cache.NewCache[string, string](shortBudget, 5*time.Minute)
	if err != nil {
		shortCache = nil
	}

	return &ContextCache{
		longCache:  longCache,
		shortCache: shortCache,
	}
}

// ---------------------------------------------------------------------------
// Key builders
// ---------------------------------------------------------------------------

// Long-cache keys (30min TTL)
func keyTier1Global() string                         { return "tier1:global" }
func keyTier1Skills(name string) string              { return "tier1:skills:" + name }
func keyTier2Project(dir string) string              { return "tier2:project:" + hashKey(dir) }
func keySessionMeta(sessionID string) string         { return "sess:meta:" + sessionID }
func keyTopicMeta(sessionID, topicID string) string  { return "topic:meta:" + sessionID + ":" + topicID }

// Short-cache keys (5min TTL)
func keyTier3File(absPath string) string             { return "tier3:file:" + hashKey(absPath) }
func keySessionToolResult(sessionID, toolName, argHash string) string {
	return "sess:tool:" + sessionID + ":" + toolName + ":" + hashKey(argHash)
}
func keyTopicSTM(sessionID, topicID string) string   { return "topic:stm:" + sessionID + ":" + topicID }
func keyAssemble(sessionID, topicID, agentID, seq string) string {
	return fmt.Sprintf("ctx:assemble:%s:%s:%s:%s", sessionID, topicID, agentID, seq)
}
func keyRender(sessionID, topicID, agentID, seq, tag string) string {
	return fmt.Sprintf("ctx:render:%s:%s:%s:%s:%s", sessionID, topicID, agentID, seq, tag)
}

func hashKey(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:12]) // 24 chars, collision-safe enough
}

// ---------------------------------------------------------------------------
// Tier 1: Global
// ---------------------------------------------------------------------------

// GetTier1Global returns cached global memory content if fresh.
func (cc *ContextCache) GetTier1Global() (string, bool) {
	return cc.get(keyTier1Global())
}

// SetTier1Global caches the global memory content.
func (cc *ContextCache) SetTier1Global(content string) {
	cc.set(keyTier1Global(), content)
}

// GetTier1Skills returns cached skill prompt for a named skill.
func (cc *ContextCache) GetTier1Skills(name string) (string, bool) {
	return cc.get(keyTier1Skills(name))
}

// SetTier1Skills caches a skill prompt.
func (cc *ContextCache) SetTier1Skills(name, prompt string) {
	cc.set(keyTier1Skills(name), prompt)
}

// InvalidateTier1Global clears the Tier 1 global cache.
func (cc *ContextCache) InvalidateTier1Global() {
	cc.del(keyTier1Global())
}

// ---------------------------------------------------------------------------
// Tier 2: Project
// ---------------------------------------------------------------------------

// GetTier2Project returns cached project memory for a directory.
func (cc *ContextCache) GetTier2Project(dir string) (string, bool) {
	return cc.get(keyTier2Project(dir))
}

// SetTier2Project caches project memory for a directory.
func (cc *ContextCache) SetTier2Project(dir, content string) {
	cc.set(keyTier2Project(dir), content)
}

// InvalidateTier2Project clears cached project memory for a directory.
func (cc *ContextCache) InvalidateTier2Project(dir string) {
	cc.del(keyTier2Project(dir))
}

// ---------------------------------------------------------------------------
// Tier 3: JIT File
// ---------------------------------------------------------------------------

// GetTier3File returns cached file content for an absolute path.
func (cc *ContextCache) GetTier3File(absPath string) (string, bool) {
	return cc.get(keyTier3File(absPath))
}

// SetTier3File caches file content for an absolute path.
func (cc *ContextCache) SetTier3File(absPath, content string) {
	cc.set(keyTier3File(absPath), content)
}

// InvalidateTier3File clears cached file content.
func (cc *ContextCache) InvalidateTier3File(absPath string) {
	cc.del(keyTier3File(absPath))
}

// ---------------------------------------------------------------------------
// Session Level — shared across all Agents in the same session
// ---------------------------------------------------------------------------

// GetSessionMeta returns cached session metadata.
func (cc *ContextCache) GetSessionMeta(sessionID string) (string, bool) {
	return cc.get(keySessionMeta(sessionID))
}

// SetSessionMeta caches session metadata (provider, mode, config, etc.).
func (cc *ContextCache) SetSessionMeta(sessionID, meta string) {
	cc.set(keySessionMeta(sessionID), meta)
}

// InvalidateSessionMeta clears cached session metadata.
func (cc *ContextCache) InvalidateSessionMeta(sessionID string) {
	cc.del(keySessionMeta(sessionID))
}

// GetSessionToolResult returns a cached tool result for the session.
// Identified by (toolName, argHash) so any Agent calling the same tool
// with the same args can reuse the result — no redundant execution.
func (cc *ContextCache) GetSessionToolResult(sessionID, toolName, argHash string) (string, bool) {
	return cc.get(keySessionToolResult(sessionID, toolName, argHash))
}

// SetSessionToolResult caches a tool result for session-wide reuse.
func (cc *ContextCache) SetSessionToolResult(sessionID, toolName, argHash, data string) {
	cc.set(keySessionToolResult(sessionID, toolName, argHash), data)
}

// PurgeSession removes all session-scoped cache entries.
func (cc *ContextCache) PurgeSession(sessionID string) {
	// Best-effort: sess:* keys expire naturally via TTL.
	// Otter doesn't support prefix deletion.
}

// ---------------------------------------------------------------------------
// Conversation Level — per-conversation within a session
// ---------------------------------------------------------------------------

// GetTopicMeta returns cached topic metadata.
func (cc *ContextCache) GetTopicMeta(sessionID, topicID string) (string, bool) {
	return cc.get(keyTopicMeta(sessionID, topicID))
}

// SetTopicMeta caches topic metadata (label, turn count, created_at).
func (cc *ContextCache) SetTopicMeta(sessionID, topicID, meta string) {
	cc.set(keyTopicMeta(sessionID, topicID), meta)
}

// GetTopicSTM returns cached STM buffer snapshot for a topic.
func (cc *ContextCache) GetTopicSTM(sessionID, topicID string) (string, bool) {
	return cc.get(keyTopicSTM(sessionID, topicID))
}

// SetTopicSTM caches an STM buffer snapshot for recovery.
func (cc *ContextCache) SetTopicSTM(sessionID, topicID, snapshot string) {
	cc.set(keyTopicSTM(sessionID, topicID), snapshot)
}

// InvalidateTopic clears topic-scoped cache entries.
func (cc *ContextCache) InvalidateTopic(sessionID, topicID string) {
	cc.del(keyTopicSTM(sessionID, topicID))
	cc.del(keyTopicMeta(sessionID, topicID))
}

// ---------------------------------------------------------------------------
// Context Assembly / Render (per-Agent, per-Conversation)
// ---------------------------------------------------------------------------

// GetAssemble returns a cached AssembledContext for a given session+conv+agent+seq.
func (cc *ContextCache) GetAssemble(sessionID, convID, agentID, seq string) (string, bool) {
	return cc.get(keyAssemble(sessionID, convID, agentID, seq))
}

// SetAssemble caches an AssembledContext snapshot.
func (cc *ContextCache) SetAssemble(sessionID, convID, agentID, seq, content string) {
	cc.set(keyAssemble(sessionID, convID, agentID, seq), content)
}

// GetRender returns a cached rendered message string (API or CLI).
func (cc *ContextCache) GetRender(sessionID, convID, agentID, seq, tag string) (string, bool) {
	return cc.get(keyRender(sessionID, convID, agentID, seq, tag))
}

// SetRender caches a rendered message string.
func (cc *ContextCache) SetRender(sessionID, convID, agentID, seq, tag, content string) {
	cc.set(keyRender(sessionID, convID, agentID, seq, tag), content)
}

// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

// Stats returns cache hit/miss counters.
func (cc *ContextCache) Stats() CacheStats {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.stats
}

// ResetStats resets hit/miss counters.
func (cc *ContextCache) ResetStats() {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.stats = CacheStats{}
}

// ---------------------------------------------------------------------------
// Internal — routes to the correct Otter cache by key prefix
// ---------------------------------------------------------------------------

// tier returns which cache tier a key belongs to.
func keyTier(key string) string {
	switch {
	case strings.HasPrefix(key, "tier1:"),
		strings.HasPrefix(key, "tier2:"),
		strings.HasPrefix(key, "sess:meta:"),
		strings.HasPrefix(key, "conv:meta:"):
		return "long"
	default:
		return "short" // tier3:, sess:tool:, conv:stm:, ctx:*
	}
}

func (cc *ContextCache) get(key string) (string, bool) {
	if cc == nil {
		return "", false
	}
	tier := keyTier(key)
	var val string
	var ok bool

	switch tier {
	case "long":
		if cc.longCache == nil {
			return "", false
		}
		val, ok = cc.longCache.Get(key)
	default: // "short"
		if cc.shortCache == nil {
			return "", false
		}
		val, ok = cc.shortCache.Get(key)
	}

	cc.mu.Lock()
	if ok {
		cc.stats.Hits++
	} else {
		cc.stats.Misses++
	}
	cc.mu.Unlock()
	return val, ok
}

func (cc *ContextCache) set(key, val string) {
	if cc == nil || strings.TrimSpace(val) == "" {
		return
	}
	tier := keyTier(key)
	switch tier {
	case "long":
		if cc.longCache != nil {
			cc.longCache.Set(key, val)
		}
	default: // "short"
		if cc.shortCache != nil {
			cc.shortCache.Set(key, val)
		}
	}
}

func (cc *ContextCache) del(key string) {
	if cc == nil {
		return
	}
	tier := keyTier(key)
	switch tier {
	case "long":
		if cc.longCache != nil {
			cc.longCache.Delete(key)
		}
	default: // "short"
		if cc.shortCache != nil {
			cc.shortCache.Delete(key)
		}
	}
}
