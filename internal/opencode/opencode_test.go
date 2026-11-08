package opencode

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tverma101/FreeTokens/internal/pricing"
	"github.com/tverma101/FreeTokens/internal/usage"

	_ "modernc.org/sqlite"
)

// fixtureTables mirrors each schema OpenCode has shipped: the older message table and the v2
// session_message table, whose message kind is a column rather than a JSON field.
var fixtureTables = map[string]string{
	"message":         `CREATE TABLE message (id TEXT PRIMARY KEY, data TEXT, time_created INTEGER)`,
	"session_message": `CREATE TABLE session_message (id TEXT PRIMARY KEY, data TEXT, time_created INTEGER, type TEXT)`,
}

func newDB(t *testing.T, tables ...string) (string, *sql.DB) {
	t.Helper()
	if len(tables) == 0 {
		tables = []string{"message", "session_message"}
	}
	path := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	for _, table := range tables {
		if _, err := db.Exec(fixtureTables[table]); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { db.Close() })
	return path, db
}

func insert(t *testing.T, db *sql.DB, id, role, providerID, modelID string, at time.Time, input, output, reasoning, cacheRead int) {
	t.Helper()
	data := fmt.Sprintf(`{"role":%q,"providerID":%q,"modelID":%q,`+
		`"tokens":{"input":%d,"output":%d,"reasoning":%d,"cache":{"read":%d,"write":0}},`+
		`"time":{"created":%d}}`,
		role, providerID, modelID, input, output, reasoning, cacheRead, at.UnixMilli())
	if _, err := db.Exec(`INSERT INTO message (id, data, time_created) VALUES (?, ?, ?)`, id, data, at.UnixMilli()); err != nil {
		t.Fatal(err)
	}
}

// insertV2 writes a message in the v2 schema, which nests the model and keeps the kind in a column.
func insertV2(t *testing.T, db *sql.DB, id, kind, providerID, modelID string, at time.Time, input, output, reasoning, cacheRead int) {
	t.Helper()
	data := fmt.Sprintf(`{"model":{"id":%q,"providerID":%q,"variant":"default"},`+
		`"tokens":{"input":%d,"output":%d,"reasoning":%d,"cache":{"read":%d,"write":0}},`+
		`"time":{"created":%d}}`,
		modelID, providerID, input, output, reasoning, cacheRead, at.UnixMilli())
	if _, err := db.Exec(`INSERT INTO session_message (id, data, time_created, type) VALUES (?, ?, ?, ?)`,
		id, data, at.UnixMilli(), kind); err != nil {
		t.Fatal(err)
	}
}

func TestScanCountsAssistantMessages(t *testing.T) {
	path, db := newDB(t, "message")
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	insert(t, db, "m1", "assistant", "anthropic", "claude-sonnet-4-5", at, 1000, 200, 50, 400)
	insert(t, db, "m2", "user", "anthropic", "claude-sonnet-4-5", at.Add(time.Minute), 900, 0, 0, 0)
	insert(t, db, "m3", "assistant", "anthropic", "claude-sonnet-4-5", at.Add(2*time.Hour), 700, 0, 0, 0)

	s := &Scanner{Path: path}
	days, err := s.Scan(context.Background(), at.Add(time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 {
		t.Fatalf("days = %+v", days)
	}
	// 1000 input, 200 output, 50 reasoning and 400 cache read; the user message and the future
	// message are not usage.
	if days[0].Tokens != 1650 {
		t.Fatalf("tokens = %d, want 1650", days[0].Tokens)
	}
	if len(days[0].Models) != 1 {
		t.Fatalf("models = %+v", days[0].Models)
	}
	if got := days[0].Models[0].Name; got != "OpenCode · Claude Sonnet 4.5" {
		t.Fatalf("name = %q", got)
	}
	if got := days[0].Models[0].Provider; got != "Anthropic" {
		t.Fatalf("provider = %q, want Anthropic", got)
	}
	periods := usage.StandardPeriods(days, at.Add(time.Hour))
	if len(periods[0].Sources) != 1 || periods[0].Sources[0].Name != "OpenCode" || periods[0].Sources[0].Tokens != 1650 {
		t.Fatalf("sources = %+v", periods[0].Sources)
	}
}

func TestScanReadsBothMessageTables(t *testing.T) {
	path, db := newDB(t)
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	insert(t, db, "m1", "assistant", "openai", "gpt-5.1-codex", at, 100, 0, 0, 0)
	insertV2(t, db, "v2-user", "user", "opencode", "deepseek-v4.1-flash-free", at, 0, 0, 0, 0)
	insertV2(t, db, "v2-assistant", "assistant", "opencode", "deepseek-v4.1-flash-free", at, 30, 40, 10, 120)

	days, err := (&Scanner{Path: path}).Scan(context.Background(), at.Add(time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 {
		t.Fatalf("days = %+v", days)
	}
	if days[0].Tokens != 300 {
		t.Fatalf("tokens = %d, want 300", days[0].Tokens)
	}
	got := map[string]bool{}
	for _, m := range days[0].Models {
		got[m.Name+"|"+m.Provider] = true
	}
	if !got["OpenCode · GPT 5.1 Codex|OpenAI"] || !got["OpenCode · Deepseek V4.1 Flash Free|DeepSeek"] {
		t.Fatalf("models = %+v", days[0].Models)
	}
}

func TestScanToleratesAMissingTable(t *testing.T) {
	path, db := newDB(t, "session_message")
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	insertV2(t, db, "v2-assistant", "assistant", "opencode", "big-pickle", at, 5, 6, 0, 7)

	days, err := (&Scanner{Path: path}).Scan(context.Background(), at.Add(time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 || days[0].Tokens != 18 {
		t.Fatalf("days = %+v", days)
	}
}

func TestOpenCodeNamesItsOwnModelsOnly(t *testing.T) {
	path, db := newDB(t, "session_message")
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	insertV2(t, db, "m1", "assistant", "opencode", "big-pickle", at, 10, 0, 0, 0)
	insertV2(t, db, "m2", "assistant", "opencode", "claude-sonnet-4-5", at, 10, 0, 0, 0)
	insertV2(t, db, "m3", "assistant", "openrouter", "mystery-1", at, 10, 0, 0, 0)

	days, err := (&Scanner{Path: path}).Scan(context.Background(), at.Add(time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range days[0].Models {
		got[m.Name] = m.Provider
	}
	// A model only OpenCode serves is labelled with its Zen gateway.
	if got["OpenCode · Big Pickle"] != "OpenCode Zen" {
		t.Errorf("own model = %q, want OpenCode Zen", got["OpenCode · Big Pickle"])
	}
	// Another vendor's model is still named from its id.
	if got["OpenCode · Claude Sonnet 4.5"] != "Anthropic" {
		t.Errorf("foreign model = %q, want Anthropic", got["OpenCode · Claude Sonnet 4.5"])
	}
	// A gateway does not claim models it merely proxied.
	if got["OpenCode · Mystery 1"] != usage.ProviderOther {
		t.Errorf("gateway model = %q, want %q", got["OpenCode · Mystery 1"], usage.ProviderOther)
	}
}

func TestMeasuredProviderBeatsTheModelName(t *testing.T) {
	path, db := newDB(t, "session_message")
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	// The model id says Anthropic, but OpenCode recorded which provider actually served it.
	insertV2(t, db, "m1", "assistant", "mistral", "claude-sonnet-4-5", at, 10, 0, 0, 0)

	days, err := (&Scanner{Path: path}).Scan(context.Background(), at.Add(time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 || len(days[0].Models) != 1 || days[0].Models[0].Provider != "Mistral" {
		t.Fatalf("models = %+v, want one Mistral row", days[0].Models)
	}
}

func TestGatewayProviderLeavesTheModelNameDeciding(t *testing.T) {
	path, db := newDB(t, "session_message")
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	// A gateway names who proxied the request, so the model still decides the vendor.
	insertV2(t, db, "m1", "assistant", "openrouter", "claude-sonnet-4-5", at, 10, 0, 0, 0)

	days, err := (&Scanner{Path: path}).Scan(context.Background(), at.Add(time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 || len(days[0].Models) != 1 || days[0].Models[0].Provider != "Anthropic" {
		t.Fatalf("models = %+v, want one Anthropic row", days[0].Models)
	}
}

func TestFoldPricesReasoningAsOutput(t *testing.T) {
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	snap := pricing.NewSnapshot(&pricing.Supplement{Pricing: map[string]pricing.Rates{
		"claude-sonnet-4-5": {Input: 3, Output: 15, CacheRead: 0.3, CacheReadExplicit: true},
	}}, nil)
	days := Fold([]Turn{{
		At: at, ModelID: "claude-sonnet-4-5",
		Input: 1000, Output: 200, Reasoning: 50, CacheRead: 400,
	}}, at.Add(time.Hour), snap)
	if len(days) != 1 || days[0].CostUSD == nil {
		t.Fatalf("days = %+v", days)
	}
	// 1000 at 3 + 400 at 0.3 + 250 output tokens (reasoning is billed as output) at 15.
	if got := *days[0].CostUSD; got < 0.00685 || got > 0.00689 {
		t.Fatalf("cost = %v, want 0.00687", got)
	}
}

func TestScanReusesCacheUntilDatabaseChanges(t *testing.T) {
	path, db := newDB(t, "message")
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	insert(t, db, "m1", "assistant", "openai", "gpt-5.1-codex", at, 100, 0, 0, 0)

	s := &Scanner{Path: path}
	days, err := s.Scan(context.Background(), at.Add(time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 || days[0].Tokens != 100 {
		t.Fatalf("days = %+v", days)
	}

	insert(t, db, "m2", "assistant", "openai", "gpt-5.1-codex", at.Add(time.Minute), 150, 0, 0, 0)
	days, err = s.Scan(context.Background(), at.Add(time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 || days[0].Tokens != 250 {
		t.Fatalf("days after insert = %+v", days)
	}
}

func TestDatabasePathFindsXDGData(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "opencode"), 0o700); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "opencode", "opencode.db")
	if err := os.WriteFile(want, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DatabasePath(); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

// A migration may copy a message into the replacement table before deleting its old row.
// One receipt in both tables must contribute one charge, not two.
func TestScanDeduplicatesMigratedMessageAcrossSchemas(t *testing.T) {
	path, db := newDB(t)
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	insert(t, db, "same-id", "assistant", "openai", "gpt-5.1-codex", at, 100, 20, 0, 0)
	insertV2(t, db, "same-id", "assistant", "openai", "gpt-5.1-codex", at, 100, 20, 0, 0)
	got, err := (&Scanner{Path: path}).Scan(context.Background(), at.Add(time.Hour), nil)
	if err != nil || len(got) != 1 || got[0].Tokens != 120 {
		t.Fatalf("migrated message counted more than once: days=%+v err=%v", got, err)
	}
}

// Unknown schema must be an explicit coverage failure: silently returning 0 tokens is misleading.
func TestScanReportsUnknownOpenCodeSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", filepath.ToSlash(path))
	if err != nil { t.Fatal(err) }
	if _, err := db.Exec("CREATE TABLE other_settings(k TEXT)"); err != nil { t.Fatal(err) }
	if err := db.Close(); err != nil { t.Fatal(err) }
	if _, err := (&Scanner{Path: path}).Scan(context.Background(), time.Now(), nil); err == nil {
		t.Fatal("unknown history schema incorrectly returned an empty successful scan")
	}
}

// A relative XDG_DATA_HOME is not a valid XDG base directory and should not
// redirect a background usage scanner into its working directory.
func TestDatabasePathRejectsRelativeXDGData(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "relative-data")
	if got := DatabasePath(); got != "" && !filepath.IsAbs(got) {
		t.Fatalf("relative XDG database path accepted: %s", got)
	}
}


// Query-side filtering is important: retaining only 30 days *after* decoding
// would repeatedly read every archived message's JSON on a busy OpenCode install.
func TestQueryMessagesOnlyReadsDisplayedWindow(t *testing.T) {
	path, db := newDB(t)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.Local)
	old := now.AddDate(-1, 0, 0)
	recent := now.Add(-time.Hour)
	insert(t, db, "legacy-old", "assistant", "openai", "gpt-5.1-codex", old, 4000, 0, 0, 0)
	insert(t, db, "legacy-new", "assistant", "openai", "gpt-5.1-codex", recent, 12, 0, 0, 0)
	insertV2(t, db, "v2-old", "assistant", "openai", "gpt-5.1-codex", old, 4000, 0, 0, 0)
	insertV2(t, db, "v2-new", "assistant", "openai", "gpt-5.1-codex", recent, 13, 0, 0, 0)
	turns, err := readMessages(context.Background(), path, usage.HistoryCutoff(now))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("got %d source rows, want two recent receipts", len(turns))
	}
	var tokens int64
	for _, turn := range turns {
		if turn.At.Before(usage.HistoryCutoff(now)) {
			t.Fatalf("archived row reached the JSON decoder: %+v", turn)
		}
		tokens += turn.Input
	}
	if tokens != 25 {
		t.Fatalf("recent tokens = %d, want 25", tokens)
	}
}

// Old schemas can have a valid JSON event time but a missing column value.
// Retain this pre-existing fallback when applying the indexed cutoff.
func TestQueryMessagesRetainsEmbeddedTimestampFallback(t *testing.T) {
	path, db := newDB(t, "message")
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.Local)
	at := now.Add(-time.Hour)
	data := fmt.Sprintf(`{"role":"assistant","modelID":"gpt-5.1-codex","tokens":{"input":42},"time":{"created":%d}}`, at.UnixMilli())
	if _, err := db.Exec(`INSERT INTO message(id, data, time_created) VALUES (?, ?, 0)`, "embedded-only", data); err != nil {
		t.Fatal(err)
	}
	turns, err := readMessages(context.Background(), path, usage.HistoryCutoff(now))
	if err != nil || len(turns) != 1 || turns[0].Input != 42 || !turns[0].At.Equal(at.Truncate(time.Millisecond)) {
		t.Fatalf("embedded-time receipt lost: turns=%+v err=%v", turns, err)
	}
}

// This benchmark isolates a changed OpenCode DB from its unchanged-file cache.
// Its older JSON rows contain extra payload that should never leave SQLite.
func BenchmarkReadMessagesActiveWindow(b *testing.B) {
	path := filepath.Join(b.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", filepath.ToSlash(path))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(fixtureTables["message"]); err != nil {
		b.Fatal(err)
	}
	if _, err := db.Exec(`CREATE INDEX message_time_created_idx ON message(time_created)`); err != nil {
		b.Fatal(err)
	}
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.Local)
	old := now.AddDate(-1, 0, 0)
	recent := now.Add(-time.Hour)
	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO message(id, data, time_created) VALUES (?, ?, ?)`)
	if err != nil {
		b.Fatal(err)
	}
	padding := strings.Repeat("x", 2048)
	for i := 0; i < 3000; i++ {
		payload := fmt.Sprintf(`{"role":"assistant","modelID":"gpt-5.1-codex","tokens":{"input":10},"extra":%q}`, padding)
		if _, err := stmt.Exec(fmt.Sprintf("archived-%d", i), payload, old.UnixMilli()); err != nil {
			b.Fatal(err)
		}
	}
	for i := 0; i < 20; i++ {
		if _, err := stmt.Exec(fmt.Sprintf("recent-%d", i), `{"role":"assistant","modelID":"gpt-5.1-codex","tokens":{"input":10}}`, recent.UnixMilli()); err != nil {
			b.Fatal(err)
		}
	}
	if err := stmt.Close(); err != nil {
		b.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		turns, err := readMessages(context.Background(), path, usage.HistoryCutoff(now))
		if err != nil || len(turns) != 20 {
			b.Fatalf("windowed read: turns=%d err=%v", len(turns), err)
		}
	}
}
