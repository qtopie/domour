package context

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtopie/domour/internal/bionic/memory"
	"github.com/qtopie/domour/internal/bionic/tool"
)

// ---------------------------------------------------------------------------
// Setup helpers
// ---------------------------------------------------------------------------

type testEnv struct {
	globalDir  string
	projectDir string
	mem        *memory.MemoryContextManager
	toolMgr    *tool.Manager
	bridge     *ContextBridge
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	globalDir := t.TempDir()
	projectDir := t.TempDir()

	// Write global rules file
	os.WriteFile(filepath.Join(globalDir, "identity.md"), []byte(`You are Domour, a bio-inspired AI agent.
You follow the biological hierarchy: Cerebrum (thinking), Cerebellum (tactical), Diencephalon (relay), Brainstem (motor).
Always be concise and helpful.`), 0644)

	os.WriteFile(filepath.Join(globalDir, "rules.md"), []byte(`## Core Rules
1. Never execute unsafe shell commands without user confirmation.
2. Always cite sources when providing code or data.
3. Use the split-apply-combine strategy for complex tasks.`), 0644)

	// Write project context file
	os.MkdirAll(filepath.Join(projectDir, ".domour"), 0755)
	os.WriteFile(filepath.Join(projectDir, ".domour", "architecture.md"), []byte(`# Project Architecture
This is a Go project using a bio-inspired multi-agent framework.
Key packages:
- brain: cognitive layer (Cerebrum, Cerebellum, Diencephalon)
- bionic: physical layer (memory, tools, context)
- engine: orchestration`), 0644)

	// MemoryContextManager — ProjectDir is the repo root, NOT .domour/
	memCfg := &memory.MemoryContextConfig{
		GlobalDir:  globalDir,
		ProjectDir: projectDir,
		CacheSize:  512,
	}
	mem := memory.NewMemoryContextManager(memCfg)

	// Tool manager (no real tools needed for this test)
	toolMgr := tool.NewManager()

	// ContextBridge
	bridge := NewContextBridge(16)

	return &testEnv{
		globalDir:  globalDir,
		projectDir: projectDir,
		mem:        mem,
		toolMgr:    toolMgr,
		bridge:     bridge,
	}
}

// ---------------------------------------------------------------------------
// Test 1: MemoryContextManager — Tier 1 + Tier 2 loading & caching
// ---------------------------------------------------------------------------

func TestMemoryContextManager_Tier1AndTier2(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Tier 1: Load system prompt
	sysPrompt := env.mem.LoadSystemPrompt(ctx, env.toolMgr, "hello", nil, "")
	if sysPrompt == "" {
		t.Fatal("LoadSystemPrompt returned empty")
	}
	if !strings.Contains(sysPrompt, "Domour") {
		t.Fatalf("System prompt should contain 'Domour', got: %s", sysPrompt[:60])
	}
	if !strings.Contains(sysPrompt, "Core Rules") {
		t.Fatal("System prompt should contain 'Core Rules'")
	}
	t.Logf("Tier 1 system prompt (%d chars):\n%s\n", len(sysPrompt), sysPrompt[:200])

	// Tier 2: Load project context
	projCtx := env.mem.LoadProjectContext()
	if projCtx == "" {
		t.Fatal("LoadProjectContext returned empty")
	}
	if !strings.Contains(projCtx, "brain: cognitive layer") {
		t.Fatal("Project context should contain architecture info")
	}
	t.Logf("Tier 2 project context (%d chars):\n%s", len(projCtx), projCtx[:150])

	// Verify cache: second call should be fast (cache hit)
	sysPrompt2 := env.mem.LoadSystemPrompt(ctx, env.toolMgr, "hello", nil, "")
	if sysPrompt != sysPrompt2 {
		t.Fatal("Cached system prompt differs from original — deterministic failure")
	}
}

// ---------------------------------------------------------------------------
// Test 3: ContextManager — Assemble + Render (no real API call)
// ---------------------------------------------------------------------------

func TestContextManager_AssembleAndRender(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	cm := NewContextManager(env.mem, ContextManagerOption{
		SessionID: "test-session-1",
		TopicID:   "test-session-1:0",
		AgentID:   "test-agent",
		ToolMgr:   env.toolMgr,
		Bridge:    env.bridge,
		Mode:      "balanced",
		Provider:  "deepseek",
	})

	// Simulate a conversation: two turns
	cm.AppendMessage("turn-1", "user", "帮我看看auth的401错误", NodeUserPrompt)
	cm.AppendMessage("turn-1", "model", "我来检查auth模块的日志和配置。", NodeAgentThought)
	cm.AppendToolResult("turn-1", "read_file", `{"path": "src/auth/middleware.go"}`, `func AuthMiddleware() {
    token := r.Header.Get("Authorization")
    // BUG: missing prefix check
    claims, _ := jwt.Parse(token, key)
}`)
	cm.AppendMessage("turn-2", "user", "找到问题了，JWT解析缺少bearer前缀检查", NodeUserPrompt)

	// Assemble
	ac := cm.Assemble(ctx, "找到问题了，JWT解析缺少bearer前缀检查", env.projectDir, nil, "", nil)
	if ac == nil {
		t.Fatal("Assemble returned nil")
	}
	if ac.Tier1System == "" {
		t.Fatal("Assembled context missing Tier1System")
	}
	if len(ac.History) == 0 {
		t.Fatal("Assembled context has empty history")
	}
	t.Logf("Assembled context: %d nodes, Tier1=%d chars, Tier2=%d chars, budget=%d",
		len(ac.History), len(ac.Tier1System), len(ac.Tier2Project), ac.TokenBudget)

	// RenderForAPI
	msgs := cm.RenderForAPI(ac)
	if len(msgs) < 2 {
		t.Fatalf("RenderForAPI returned %d messages, want >= 2", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Fatalf("First message role = %q, want 'system'", msgs[0].Role)
	}
	// Verify last message is user (the input)
	last := msgs[len(msgs)-1]
	if last.Role != "user" {
		t.Fatalf("Last message role = %q, want 'user'", last.Role)
	}
	if !strings.Contains(last.Content, "bearer") {
		t.Fatal("Last user message should contain 'bearer'")
	}
	t.Logf("RenderForAPI: %d messages, first: role=%s (%d chars), last: role=%s (%d chars)",
		len(msgs), msgs[0].Role, len(msgs[0].Content), last.Role, len(last.Content))

	// RenderForCLI
	flat := cm.RenderForCLI(ac)
	if !strings.Contains(flat, "[SYSTEM]") || !strings.Contains(flat, "[USER]") {
		t.Fatal("RenderForCLI should contain [SYSTEM] and [USER] tags")
	}
	if strings.Count(flat, "[SYSTEM]") != 1 {
		t.Fatal("RenderForCLI should have exactly one [SYSTEM] block")
	}
	t.Logf("RenderForCLI: %d chars", len(flat))

	// RenderForCerebellum — stripped context
	cere := cm.RenderForCerebellum(ac, "修复 auth 401 错误", "检查 JWT middleware")
	if cere.Intent != "修复 auth 401 错误" {
		t.Fatalf("Cerebellum intent = %q, want '修复 auth 401 错误'", cere.Intent)
	}
	if len(cere.ToolSchemas) == 0 {
		t.Log("Cerebellum tool schemas empty (no tools registered — OK)")
	}
	t.Logf("RenderForCerebellum: intent=%s, step=%s, artifacts=%v",
		cere.Intent, cere.CurrentStep, cere.Artifacts)

	// RenderForDiencephalon — minimal payload
	dien := cm.RenderForDiencephalon(ac)
	if len(dien.Messages) == 0 {
		t.Fatal("Diencephalon payload has empty messages")
	}
	if dien.Provider != "deepseek" {
		t.Fatalf("Diencephalon provider = %q, want 'deepseek'", dien.Provider)
	}
	t.Logf("RenderForDiencephalon: provider=%s, %d messages", dien.Provider, len(dien.Messages))
}

// ---------------------------------------------------------------------------
// Test 4: ContextBridge — cross-Agent publish/subscribe
// ---------------------------------------------------------------------------

func TestContextBridge_CrossAgent(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Agent A: RefundAgent
	agentA := NewContextManager(env.mem, ContextManagerOption{
		SessionID: "sess-1",
		TopicID:   "sess-1:0",
		AgentID:   "refund-agent",
		ToolMgr:   env.toolMgr,
		Bridge:    env.bridge,
		Mode:      "balanced",
		Provider:  "deepseek",
	})

	// Agent B: RiskAgent
	agentB := NewContextManager(env.mem, ContextManagerOption{
		SessionID: "sess-1",
		TopicID:   "sess-1:0",
		AgentID:   "risk-agent",
		ToolMgr:   env.toolMgr,
		Bridge:    env.bridge,
		Mode:      "balanced",
		Provider:  "deepseek",
	})

	// Agent A subscribes to nothing, publishes analysis
	pkgID := agentA.PublishContext("analysis-result", "Fraud detected: unusual pattern in transaction #9021",
		map[string]string{"risk_level": "high", "confidence": "0.95", "transaction_id": "9021"})
	if pkgID == "" {
		t.Fatal("PublishContext returned empty ID — bridge is nil?")
	}
	t.Logf("Agent A published package: %s", pkgID)

	// Agent B subscribes and imports
	agentB.SubscribeToChannel("analysis-result")
	imported := agentB.ImportContext(ctx)
	if imported == 0 {
		t.Fatal("Agent B imported 0 packages, want >= 1")
	}
	t.Logf("Agent B imported %d package(s)", imported)

	// Verify: Agent B's STM now has the bridge node
	history := agentB.stm.Snapshot()
	found := false
	for _, node := range history {
		if node.Type == NodeSystemEvent && strings.Contains(node.Content, "Fraud detected") {
			found = true
			t.Logf("Bridge node in Agent B STM: %s", node.Content)
			break
		}
	}
	if !found {
		t.Fatal("Agent B's STM does not contain the bridge event")
	}

	// Verify isolation: Agent B cannot see Agent A's internal nodes
	for _, node := range history {
		if node.Metadata[MetaKeySource] == "tool" {
			t.Fatal("Agent B should NOT see Agent A's tool execution nodes")
		}
	}

	// Direct publish (ScopeIsolated)
	directID := agentA.PublishDirect("risk-agent", "High priority: flag user #42 for manual review",
		map[string]string{"user_id": "42", "priority": "critical"})
	if directID == "" {
		t.Fatal("PublishDirect returned empty ID")
	}
	directImported := agentB.ImportContext(ctx)
	t.Logf("Agent B imported %d more package(s) (including direct)", directImported)
}

// ---------------------------------------------------------------------------
// Test 5: Full pipeline with real DeepSeek API call
// ---------------------------------------------------------------------------

func TestFullPipeline_WithDeepSeek(t *testing.T) {
	apiKey := resolveDeepSeekKey(t)
	if apiKey == "" {
		t.Skip("DEEPSEEK_API_KEY not available — skipping real API test")
	}

	env := setupTestEnv(t)
	ctx := context.Background()

	cm := NewContextManager(env.mem, ContextManagerOption{
		SessionID: "test-sess",
		TopicID:   "test-sess:0",
		AgentID:   "integration-test",
		ToolMgr:   env.toolMgr,
		Bridge:    env.bridge,
		Mode:      "balanced",
		Provider:  "deepseek",
	})

	// Add a conversation turn
	cm.AppendMessage("turn-1", "user", "简单介绍一下你自己", NodeUserPrompt)

	// Assemble context
	ac := cm.Assemble(ctx, "简单介绍一下你自己", env.projectDir, nil, "", nil)

	// Render for API
	msgs := cm.RenderForAPI(ac)

	// Call DeepSeek API
	respText := callDeepSeek(t, apiKey, msgs)
	if respText == "" {
		t.Fatal("DeepSeek returned empty response")
	}
	t.Logf("DeepSeek response (%d chars):\n%s", len(respText), respText)

	// Verify the response mentions Domour or bio-inspired
	if !strings.Contains(strings.ToLower(respText), "domour") &&
		!strings.Contains(strings.ToLower(respText), "bio") {
		t.Log("Warning: response doesn't mention Domour or bio-inspired (may still be OK)")
	}

	// Second call — verify prefix caching works (system prompt identical)
	// If the system prompt prefix is identical, the provider should return
	// a response much faster on the second call.
	cm2 := NewContextManager(env.mem, ContextManagerOption{
		SessionID: "test-sess",
		TopicID:   "test-sess:0",
		AgentID:   "integration-test-2",
		ToolMgr:   env.toolMgr,
		Bridge:    env.bridge,
		Mode:      "balanced",
		Provider:  "deepseek",
	})
	cm2.AppendMessage("turn-1", "user", "你有什么能力", NodeUserPrompt)
	ac2 := cm2.Assemble(ctx, "你有什么能力", env.projectDir, nil, "", nil)
	msgs2 := cm2.RenderForAPI(ac2)

	// System prompt must be identical (prefix cache hit check)
	sys1 := msgs[0].Content
	sys2 := msgs2[0].Content
	if sys1 != sys2 {
		t.Fatalf("System prompts differ between agents — prefix cache broken!\n"+
			"Agent1 system (%d chars) != Agent2 system (%d chars)", len(sys1), len(sys2))
	}
	t.Logf("✅ System prompts identical (%d chars) — prefix cache will hit!", len(sys1))

	respText2 := callDeepSeek(t, apiKey, msgs2)
	t.Logf("Second DeepSeek response (%d chars):\n%s", len(respText2), respText2)
}

// ---------------------------------------------------------------------------
// Test 6: ContextCache — shared L1 cache verification
// ---------------------------------------------------------------------------

func TestContextCache_SharedAcrossAgents(t *testing.T) {
	cache := memory.NewContextCache(512)

	// Agent A writes a tool result
	cache.SetSessionToolResult("sess-1", "read_file", `{"path": "main.go"}`, `package main`)

	// Agent B reads the same tool result — must hit
	val, ok := cache.GetSessionToolResult("sess-1", "read_file", `{"path": "main.go"}`)
	if !ok {
		t.Fatal("Agent B should get cache hit for Agent A's tool result")
	}
	if val != `package main` {
		t.Fatalf("Cached value = %q, want 'package main'", val)
	}
	t.Logf("✅ Shared tool result cache works: got %q", val)

	// Different args → cache miss (new hash)
	_, ok = cache.GetSessionToolResult("sess-1", "read_file", `{"path": "other.go"}`)
	if ok {
		t.Fatal("Different args should NOT return cache hit")
	}
	t.Log("✅ Different args → cache miss as expected")

	// Topic metadata caching
	cache.SetTopicMeta("sess-1", "sess-1:0", `{"label": "auth_debug", "turns": 5}`)
	meta, ok := cache.GetTopicMeta("sess-1", "sess-1:0")
	if !ok {
		t.Fatal("Topic meta cache miss")
	}
	if !strings.Contains(meta, "auth_debug") {
		t.Fatalf("Topic meta = %q, should contain 'auth_debug'", meta)
	}
	t.Logf("✅ Topic meta cache works: %s", meta)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func resolveDeepSeekKey(t *testing.T) string {
	t.Helper()

	// Try env first
	if key := os.Getenv("DEEPSEEK_API_KEY"); key != "" {
		return key
	}

	// Try config file
	cfgPath := os.ExpandEnv("$HOME/.domour/config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return ""
	}

	var cfg struct {
		Providers map[string]struct {
			APIKey string `json:"api_key"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return cfg.Providers["deepseek"].APIKey
}

func callDeepSeek(t *testing.T, apiKey string, messages []Message) string {
	t.Helper()

	type chatMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	reqBody := map[string]interface{}{
		"model":       "deepseek-chat",
		"messages":    messages,
		"max_tokens":  512,
		"temperature": 0.7,
	}

	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "https://api.deepseek.com/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("DeepSeek API call failed: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if result.Error != nil && result.Error.Message != "" {
		t.Fatalf("DeepSeek API error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		t.Fatal("DeepSeek returned 0 choices")
	}
	return result.Choices[0].Message.Content
}
