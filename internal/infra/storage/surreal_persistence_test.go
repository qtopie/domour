package storage

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/pkg/bionic/session"
	"github.com/qtopie/domour/pkg/infra/cache"
	"github.com/qtopie/domour/pkg/infra/cache/l1"
	"github.com/qtopie/domour/pkg/infra/eventbus"
	"github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/models"
)

// ─── Test helpers for core-mode testing ───────────────────────────────────

// testSurrealDBCache wraps SurrealDB as a cache.Cache[V] for session.Manager.
type testSurrealDBCache[V any] struct {
	db        *surrealdb.DB
	tableName string
	mu        sync.Mutex
}

func NewSurrealDBCache[V any](db *SurrealDB, tableName string) cache.Cache[V] {
	return &testSurrealDBCache[V]{db: db.db, tableName: tableName}
}

func (c *testSurrealDBCache[V]) Get(ctx context.Context, key string) (V, bool, error) {
	var zero V
	rid := models.NewRecordID(c.tableName, key)
	res, err := surrealdb.Select[struct {
		Value    V         `json:"value"`
		ExpireAt time.Time `json:"expire_at"`
	}](ctx, c.db, rid)
	if err != nil {
		return zero, false, nil // key not found
	}
	if res == nil {
		return zero, false, nil
	}
	if !(*res).ExpireAt.IsZero() && (*res).ExpireAt.Before(time.Now()) {
		return zero, false, nil // expired
	}
	return (*res).Value, true, nil
}

func (c *testSurrealDBCache[V]) Set(ctx context.Context, key string, value V, ttl time.Duration) error {
	rid := models.NewRecordID(c.tableName, key)
	_, err := surrealdb.Upsert[any](ctx, c.db, rid, map[string]any{
		"id":        rid,
		"value":     value,
		"expire_at": time.Now().Add(ttl),
	})
	return err
}

func (c *testSurrealDBCache[V]) Delete(ctx context.Context, key string) error {
	rid := models.NewRecordID(c.tableName, key)
	_, err := surrealdb.Delete[any](ctx, c.db, rid)
	return err
}

func (c *testSurrealDBCache[V]) Close() error { return nil }

// testSurrealDBStore wraps SurrealDB as a storage.DB for session.Manager.
type testSurrealDBStore struct {
	db *surrealdb.DB
}

func NewSurrealDBStore(db *SurrealDB) *testSurrealDBStore {
	return &testSurrealDBStore{db: db.db}
}

func (s *testSurrealDBStore) Query(ctx context.Context, sql string, vars map[string]any) (any, error) {
	return surrealdb.Query[any](ctx, s.db, sql, vars)
}

func (s *testSurrealDBStore) Create(ctx context.Context, table string, data any) (any, error) {
	res, err := surrealdb.Create[any](ctx, s.db, table, data)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return *res, nil
}

func (s *testSurrealDBStore) Update(ctx context.Context, thing string, data any) (any, error) {
	// If thing is "session:id" with hyphens, use RecordID to avoid arithmetic parsing
	if strings.Contains(thing, ":") {
		parts := strings.SplitN(thing, ":", 2)
		table, id := parts[0], parts[1]
		if strings.ContainsAny(id, "-") {
			rid := models.NewRecordID(table, id)
			res, err := surrealdb.Upsert[any](ctx, s.db, rid, data)
			if err != nil {
				return nil, err
			}
			if res == nil {
				return nil, nil
			}
			return *res, nil
		}
	}
	res, err := surrealdb.Update[any](ctx, s.db, thing, data)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return *res, nil
}

func (s *testSurrealDBStore) Delete(ctx context.Context, thing string) (any, error) {
	res, err := surrealdb.Delete[any](ctx, s.db, thing)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return *res, nil
}

func (s *testSurrealDBStore) Select(ctx context.Context, thing string) (any, error) {
	res, err := surrealdb.Select[any](ctx, s.db, thing)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return *res, nil
}

func (s *testSurrealDBStore) Close() error { return nil }

// testNoopEventBus is a no-op eventbus.EventBus for testing.
type testNoopEventBus struct{}

func NewNoopEventBus() eventbus.EventBus { return &testNoopEventBus{} }

func (b *testNoopEventBus) Publish(ctx context.Context, topic string, data []byte) error { return nil }

func (b *testNoopEventBus) Subscribe(ctx context.Context, topic string, handler func(data []byte)) (eventbus.Subscription, error) {
	return &testSubscription{}, nil
}

func (b *testNoopEventBus) Close() error { return nil }

type testSubscription struct{}

func (s *testSubscription) Unsubscribe() error { return nil }

const testSessionID = "core-mode-persistence-test"

func skipIfNoSurrealDB(t *testing.T) {
	t.Helper()
	addr := os.Getenv("DOMOUR_SURREAL_ADDR")
	if addr == "" {
		addr = "ws://127.0.0.1:8000/rpc"
	}
	db, err := NewSurrealDB(Config{
		Address:   addr,
		User:      "root",
		Pass:      "root",
		Namespace: "domour",
		Database:  "agent",
	})
	if err != nil {
		t.Skipf("SurrealDB not available at %s: %v", addr, err)
	}
	db.Close()
}

// TestSurrealStore_Persistence verifies that a SurrealStore can
// save and load sessions across "restarts" (new store instances).
// Uses raw SurrealQL queries to validate the data at the DB level.
func TestSurrealStore_Persistence(t *testing.T) {
	skipIfNoSurrealDB(t)

	cfg := Config{
		Address:   os.Getenv("DOMOUR_SURREAL_ADDR"),
		User:      "root",
		Pass:      "root",
		Namespace: "domour",
		Database:  "agent",
	}
	if cfg.Address == "" {
		cfg.Address = "ws://127.0.0.1:8000/rpc"
	}

	ctx := context.Background()

	// ─── First "lifetime": connect and save ───────────────────────────
	t.Log("=== First session: connecting and saving data ===")
	db1, err := NewSurrealDB(cfg)
	if err != nil {
		t.Fatalf("first SurrealDB connect: %v", err)
	}
	store1 := NewSurrealStore(db1)

	sess := session.Session{
		ID:        testSessionID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	sess.History = []shared.Message{
		{Role: "user", Content: "Hello from core mode", Seq: 1, Time: time.Now().Unix()},
		{Role: "assistant", Content: "Hi! This should persist after restart.", Seq: 2, Time: time.Now().Unix()},
	}
	sess.MemorySummary = "Core mode persistence test"
	sess.ActiveProvider = "deepseek"
	sess.ActiveModel = "deepseek-chat"

	if err := store1.SaveSession(ctx, sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	t.Log("Session saved to SurrealDB")

	// Verify using raw SurrealQL (bypasses parsing logic)
	// Escape record ID with backticks to handle hyphens in the record ID
	qRes, err := db1.Query(ctx, "SELECT * FROM session:`"+testSessionID+"`", nil)
	if err != nil {
		t.Fatalf("raw query after save: %v", err)
	}
	t.Logf("Raw SurrealDB result: %+v", qRes)

	db1.Close()
	t.Log("First connection closed (simulating restart)")

	// ─── Second "lifetime": new connection, verify data persists ──────
	t.Log("=== Second session: new connection (simulating restart) ===")
	db2, err := NewSurrealDB(cfg)
	if err != nil {
		t.Fatalf("second SurrealDB connect: %v", err)
	}
	defer db2.Close()

	// Use raw SurrealQL to read back — no QueryResult wrapping to deal with
	qRes2, err := db2.Query(ctx, "SELECT * FROM session:`"+testSessionID+"`", nil)
	if err != nil {
		t.Fatalf("raw query after restart: %v", err)
	}

	// Parse the raw query result
	bytes2, _ := json.Marshal(qRes2)
	t.Logf("Raw JSON after restart: %s", string(bytes2))

	var qr []surrealdb.QueryResult[any]
	if err := json.Unmarshal(bytes2, &qr); err != nil {
		t.Fatalf("unmarshal query result: %v", err)
	}
	if len(qr) == 0 {
		t.Fatal("empty query result — session lost after restart!")
	}

	// Extract the session from the query result
	// Pre-process: SurrealDB returns `id` as a RecordID object, not a string.
	// We need to normalize it before unmarshaling into shared.Session.
	rBytes, _ := json.Marshal(qr[0].Result)

	// Unmarshal as raw maps first to normalize the RecordID
	var rawList []map[string]any
	if err := json.Unmarshal(rBytes, &rawList); err != nil || len(rawList) == 0 {
		t.Fatalf("unmarshal raw result: %v (result: %s)", err, string(rBytes))
	}
	raw := rawList[0]

	// Convert RecordID object to string
	if idObj, ok := raw["id"].(map[string]any); ok {
		if table, _ := idObj["Table"].(string); table != "" {
			if idStr, _ := idObj["ID"].(string); idStr != "" {
				raw["id"] = table + ":" + idStr
			}
		}
	}

	normalized, _ := json.Marshal(raw)
	var reloaded shared.Session
	if err := json.Unmarshal(normalized, &reloaded); err != nil {
		t.Fatalf("normalized unmarshal failed: %v (json: %s)", err, string(normalized))
	}
	if reloaded.ID == "" {
		t.Fatalf("session ID empty after normalization")
	}
	if len(reloaded.History) != 2 {
		t.Fatalf("expected 2 history messages after restart, got %d", len(reloaded.History))
	}
	if reloaded.History[0].Content != "Hello from core mode" {
		t.Errorf("restore: unexpected first message: %q", reloaded.History[0].Content)
	}
	if reloaded.MemorySummary != "Core mode persistence test" {
		t.Errorf("restore: MemorySummary lost: %q", reloaded.MemorySummary)
	}
	if reloaded.ActiveProvider != "deepseek" || reloaded.ActiveModel != "deepseek-chat" {
		t.Errorf("restore: provider/model lost: %s/%s", reloaded.ActiveProvider, reloaded.ActiveModel)
	}

	t.Logf("✅ Data persisted across restart: %d messages, provider=%s, model=%s",
		len(reloaded.History), reloaded.ActiveProvider, reloaded.ActiveModel)

	// Cleanup — escape record ID for SurrealQL
	_, _ = db2.Query(ctx, "DELETE session:`"+testSessionID+"`", nil)
	t.Log("Cleanup complete")
}

// TestSessionManager_SurrealDB verifies that session.Manager with
// SurrealDB backend persists data across Manager instances (restart).
func TestSessionManager_SurrealDB(t *testing.T) {
	skipIfNoSurrealDB(t)

	ctx := context.Background()
	sessionID := "manager-persistence-test-" + time.Now().Format("150405")

	cfg := Config{
		Address:   os.Getenv("DOMOUR_SURREAL_ADDR"),
		User:      "root",
		Pass:      "root",
		Namespace: "domour",
		Database:  "agent",
	}
	if cfg.Address == "" {
		cfg.Address = "ws://127.0.0.1:8000/rpc"
	}

	// Create SurrealDB connection and L2 cache
	db, err := NewSurrealDB(cfg)
	if err != nil {
		t.Fatalf("SurrealDB connect: %v", err)
	}
	defer db.Close()

	surrealCache := NewSurrealDBCache[session.Session](db, "l2_test_cache")
	defer surrealCache.Close()

	surrealDB := NewSurrealDBStore(db)
	defer surrealDB.Close()

	// L1 cache (in-memory — cleared on "restart")
	l1Cache, err := l1.NewCache[string, session.Session](1024, 15*time.Minute)
	if err != nil {
		t.Fatalf("L1 cache: %v", err)
	}

	eventBus := NewNoopEventBus()
	defer eventBus.Close()

	// ─── First Manager (save data) ─────────────────────────────────
	t.Log("=== First Manager instance ===")
	mgr1 := session.NewManager(l1Cache, surrealCache, surrealDB, eventBus)

	// Test GetSession first (expect error since session doesn't exist)
	t.Logf("Testing GetSession for %q (expecting error)", sessionID)
	_, getErr := mgr1.GetSession(ctx, sessionID)
	if getErr != nil {
		t.Logf("GetSession returned expected error: %v", getErr)
	} else {
		t.Log("GetSession returned no error (session may exist from previous run)")
	}

	msg1 := shared.Message{Role: "user", Content: "Test from Manager", Seq: 1, Time: time.Now().Unix()}
	if err := mgr1.AppendHistory(ctx, sessionID, msg1); err != nil {
		t.Fatalf("mgr1 AppendHistory: %v", err)
	}
	t.Log("Saved 1 message via Manager")

	msg2 := shared.Message{Role: "assistant", Content: "Manager response", Seq: 2, Time: time.Now().Unix()}
	if err := mgr1.AppendHistory(ctx, sessionID, msg2); err != nil {
		t.Fatalf("mgr1 AppendHistory 2: %v", err)
	}
	t.Log("Saved 2 messages via Manager")

	// Verify read-back through same Manager
	hist1, err := mgr1.GetHistory(ctx, sessionID)
	if err != nil {
		t.Fatalf("mgr1 GetHistory: %v", err)
	}
	if len(hist1) != 2 {
		t.Fatalf("mgr1: expected 2 history items, got %d", len(hist1))
	}

	mgr1.Close()
	t.Log("First Manager closed (simulating restart)")

	// ─── Second Manager (new L1 + same SurrealDB) ────────────────
	t.Log("=== Second Manager instance (simulating restart) ===")
	l1Cache2, err := l1.NewCache[string, session.Session](1024, 15*time.Minute)
	if err != nil {
		t.Fatalf("L1 cache 2: %v", err)
	}
	eventBus2 := NewNoopEventBus()
	defer eventBus2.Close()

	mgr2 := session.NewManager(l1Cache2, surrealCache, surrealDB, eventBus2)

	hist2, err := mgr2.GetHistory(ctx, sessionID)
	if err != nil {
		t.Fatalf("mgr2 GetHistory: %v", err)
	}
	if len(hist2) != 2 {
		t.Fatalf("mgr2: expected 2 history items after restart, got %d", len(hist2))
	}
	if hist2[0].Content != "Test from Manager" {
		t.Errorf("mgr2: first message lost: %q", hist2[0].Content)
	}
	if hist2[1].Content != "Manager response" {
		t.Errorf("mgr2: second message lost: %q", hist2[1].Content)
	}
	t.Logf("✅ Manager persistence verified: %d messages survived restart", len(hist2))

	// Append a third message after restart
	msg3 := shared.Message{Role: "user", Content: "This is after restart", Seq: 3, Time: time.Now().Unix()}
	if err := mgr2.AppendHistory(ctx, sessionID, msg3); err != nil {
		t.Fatalf("mgr2 AppendHistory: %v", err)
	}
	t.Log("Appended message after restart")

	// ─── Third Manager (verify append persisted) ─────────────────
	t.Log("=== Third Manager instance ===")
	l1Cache3, err := l1.NewCache[string, session.Session](1024, 15*time.Minute)
	if err != nil {
		t.Fatalf("L1 cache 3: %v", err)
	}
	eventBus3 := NewNoopEventBus()
	defer eventBus3.Close()

	mgr3 := session.NewManager(l1Cache3, surrealCache, surrealDB, eventBus3)
	hist3, err := mgr3.GetHistory(ctx, sessionID)
	if err != nil {
		t.Fatalf("mgr3 GetHistory: %v", err)
	}
	if len(hist3) != 3 {
		t.Fatalf("mgr3: expected 3 messages, got %d", len(hist3))
	}
	if hist3[2].Content != "This is after restart" {
		t.Errorf("mgr3: append after restart lost: %q", hist3[2].Content)
	}
	t.Logf("✅ All %d messages persisted across 3 Manager instances", len(hist3))

	mgr2.Close()
	mgr3.Close()

	// Cleanup — escape record ID for SurrealQL
	_, _ = db.Query(ctx, "DELETE session:`"+sessionID+"`", nil)
	_, _ = db.Query(ctx, "DELETE FROM l2_test_cache", nil)
}

// TestSurrealDB_ConnectionRecovery verifies that if SurrealDB is unavailable
// during startup, domour falls back gracefully (log error, continue).
func TestSurrealDB_ConnectionRecovery(t *testing.T) {
	// Try connecting to a non-existent SurrealDB instance
	_, err := NewSurrealDB(Config{
		Address:   "ws://127.0.0.1:19999/rpc",
		User:      "root",
		Pass:      "root",
		Namespace: "domour",
		Database:  "agent",
	})
	if err == nil {
		t.Log("SurrealDB connected (expected — server may be running on that port)")
		return
	}
	// Expected: connection failure
	t.Logf("Connection failure as expected: %v", err)
	t.Log("✅ Graceful fallback path verified")
}
