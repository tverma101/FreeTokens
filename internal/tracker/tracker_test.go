package tracker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/tverma101/FreeTokens/internal/antigravity"
	"github.com/tverma101/FreeTokens/internal/claude"
	"github.com/tverma101/FreeTokens/internal/freebuff"
	"github.com/tverma101/FreeTokens/internal/opencode"
	"github.com/tverma101/FreeTokens/internal/pricing"
	"github.com/tverma101/FreeTokens/internal/usage"
)

// offlinePricing is a store whose feeds cannot be fetched, so a refresh never touches the network.
func offlinePricing(t *testing.T) *pricing.Store {
	t.Helper()
	store := pricing.NewStore(t.TempDir(), "")
	store.HTTP = &http.Client{Transport: failingTransport{}}
	return store
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("offline in tests")
}

// noAuth is an AuthStore with no credentials anywhere, so the hosted and catalog calls are skipped.
func noAuth(t *testing.T) freebuff.AuthStore {
	t.Helper()
	return freebuff.AuthStore{
		Getenv:   func(string) string { return "" },
		HomeDir:  func() (string, error) { return t.TempDir(), nil },
		ReadFile: func(string) ([]byte, error) { return nil, os.ErrNotExist },
	}
}

// corruptDesktopDB writes a database Freebuff Desktop could plausibly leave behind whose usage row
// cannot be read, which makes the Desktop fold fail with ErrLocalHistory.
func corruptDesktopDB(t *testing.T, root string, ts time.Time) {
	t.Helper()
	dir := filepath.Join(root, "Freebuff", "projects", "spyro")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dir, "desktop-v2.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	statements := []string{
		`CREATE TABLE threads(id TEXT, model TEXT, harness_id TEXT, world_snapshot TEXT)`,
		`CREATE TABLE messages(thread_id TEXT, seq INTEGER, request_id TEXT, ts INTEGER, metrics_json TEXT, role TEXT)`,
		`INSERT INTO threads VALUES ('t1', 'deepseek/deepseek-v4-flash', 'codebuff', NULL)`,
		fmt.Sprintf(`INSERT INTO messages VALUES ('t1', 1, NULL, %d, '{"usage":{"inputTokens":"not a number","outputTokens":5}}', 'assistant')`, ts.UnixMilli()),
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

func claudeTranscript(t *testing.T, root string, at time.Time) {
	t.Helper()
	path := filepath.Join(root, "project", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"type":"assistant","timestamp":%q,"sessionId":"s1","message":{"id":"msg_1",`+
		`"model":"claude-sonnet-4-5-20250929","usage":{"input_tokens":100,"output_tokens":50,`+
		`"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
		at.Format(time.RFC3339Nano))
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func dayByKey(days []usage.Day, key string) *usage.Day {
	for i := range days {
		if days[i].Date == key {
			return &days[i]
		}
	}
	return nil
}

// An unreadable Freebuff Desktop database used to return the snapshot before any other reader ran,
// which silently dropped every independent source on the machine. It must now cost only its own rows.
func TestUnreadableDesktopDatabaseDoesNotHideOtherSources(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local)
	at := now.Add(-time.Hour)

	desktopRoot := t.TempDir()
	corruptDesktopDB(t, desktopRoot, at)
	claudeRoot := t.TempDir()
	claudeTranscript(t, claudeRoot, at)

	tr := &Tracker{
		Auth:    noAuth(t),
		Pricing: offlinePricing(t),
		Catalog: &freebuff.ModelCatalog{CachePath: filepath.Join(t.TempDir(), "models.json")},
		Desktop: &freebuff.DesktopScanner{Roots: []string{desktopRoot}},
		Claude:  &claude.Scanner{Root: claudeRoot},
		Now:     func() time.Time { return now },
	}

	snap := tr.Refresh(context.Background(), false)

	day := dayByKey(snap.Days, usage.DayKey(at))
	if day == nil {
		t.Fatalf("days = %+v, want the Claude Code day", snap.Days)
	}
	if day.Tokens != 150 {
		t.Fatalf("tokens = %d, want the 150 measured by Claude Code", day.Tokens)
	}
	if !strings.Contains(snap.HistoryWarning, "Freebuff Desktop's measured token history") {
		t.Fatalf("warning = %q, want the Freebuff Desktop failure named", snap.HistoryWarning)
	}
	if strings.Contains(snap.HistoryWarning, "Claude Code history") {
		t.Fatalf("warning = %q, want no Claude Code failure", snap.HistoryWarning)
	}
}

// Two failing readers name both failures rather than letting the last one win.
func TestWarningsFromIndependentSourcesAreJoined(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local)
	desktopRoot := t.TempDir()
	corruptDesktopDB(t, desktopRoot, now.Add(-time.Hour))

	// A readable file that is not a SQLite database: OpenCode's first query fails.
	notADatabase := filepath.Join(t.TempDir(), "opencode.db")
	if err := os.WriteFile(notADatabase, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}

	tr := &Tracker{
		Auth:     noAuth(t),
		Pricing:  offlinePricing(t),
		Catalog:  &freebuff.ModelCatalog{CachePath: filepath.Join(t.TempDir(), "models.json")},
		Desktop:  &freebuff.DesktopScanner{Roots: []string{desktopRoot}},
		OpenCode: &opencode.Scanner{Path: notADatabase},
		Now:      func() time.Time { return now },
	}
	snap := tr.Refresh(context.Background(), false)
	if !strings.Contains(snap.HistoryWarning, "Freebuff Desktop's measured token history") || !strings.Contains(snap.HistoryWarning, "OpenCode history") {
		t.Fatalf("warning = %q, want both source failures", snap.HistoryWarning)
	}
}

// A signed-out Freebuff only makes Freebuff's own numbers a lower bound. Claude Code measured its
// day completely, so that day must not be flagged partial because the login is missing.
func TestSignedOutFreebuffLeavesMeasuredSourcesComplete(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local)
	at := now.Add(-time.Hour)
	claudeRoot := t.TempDir()
	claudeTranscript(t, claudeRoot, at)

	tr := &Tracker{
		Auth:    noAuth(t),
		Pricing: offlinePricing(t),
		Catalog: &freebuff.ModelCatalog{CachePath: filepath.Join(t.TempDir(), "models.json")},
		Desktop: &freebuff.DesktopScanner{Roots: []string{t.TempDir()}},
		Claude:  &claude.Scanner{Root: claudeRoot},
		Now:     func() time.Time { return now },
	}
	snap := tr.Refresh(context.Background(), false)

	day := dayByKey(snap.Days, usage.DayKey(at))
	if day == nil || day.Tokens != 150 {
		t.Fatalf("days = %+v, want the 150 tokens Claude Code measured", snap.Days)
	}
	if day.Partial {
		t.Fatalf("day = %+v, want a complete Claude Code day while Freebuff is signed out", day)
	}
	if strings.Contains(snap.HistoryWarning, "no complete receipt") {
		t.Fatalf("warning = %q, want no lower-bound claim about a measured day", snap.HistoryWarning)
	}
}

// A source that skips a file still returns the days it could read. That loss has to reach the user,
// which a log line does not do.
func TestUnreadableSourceIsNamedInTheWarning(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local)
	root := t.TempDir()
	dir := filepath.Join(root, "conversations")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.db"), []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}

	tr := &Tracker{
		Auth:        noAuth(t),
		Pricing:     offlinePricing(t),
		Catalog:     &freebuff.ModelCatalog{CachePath: filepath.Join(t.TempDir(), "models.json")},
		Desktop:     &freebuff.DesktopScanner{Roots: []string{t.TempDir()}},
		Antigravity: &antigravity.Scanner{Roots: []antigravity.Root{{Dir: root, Source: "Antigravity"}}},
		Now:         func() time.Time { return now },
	}
	snap := tr.Refresh(context.Background(), false)
	if !strings.Contains(snap.HistoryWarning, "1 Antigravity conversation database(s) couldn't be read") {
		t.Fatalf("warning = %q, want the unreadable conversation named", snap.HistoryWarning)
	}
}
